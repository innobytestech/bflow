// <!-- generado por bflow render: no lo edites; cambia bflow.yaml (agents) o el archivo extra y vuelve a correrlo -->
// Plugin de bflow para OpenCode: guard antes de cada herramienta y tokens por sesion.
// Solo le pasa datos a bflow (por stdin); toda regla vive en `bflow guard`.
import { appendFileSync, existsSync, mkdirSync } from "node:fs"
import { join } from "node:path"

const GUARDED = new Set(["bash", "edit", "write", "multiedit", "patch", "apply_patch"])
const safe = (id) => String(id ?? "").replace(/[^A-Za-z0-9_-]/g, "")

export const BflowPlugin = async ({ client, directory }) => {
  const sessions = new Map() // sessionID -> { parentID, agent }
  let warned = false
  const warn = (msg) => {
    if (warned) return
    warned = true
    const text = "bflow: " + msg
    Promise.resolve(client?.tui?.showToast?.({ body: { message: text, variant: "warning" } })).catch(() => console.error(text))
  }
  const note = (id, patch) => sessions.set(id, { ...sessions.get(id), ...patch })
  const resolve = async (id) => {
    if (!sessions.get(id)?.known) {
      try {
        const r = await client.session.get({ path: { id } })
        note(id, { parentID: r?.data?.parentID ?? "", known: true })
      } catch {}
    }
    return sessions.get(id) ?? {}
  }
  const run = (args, input) => {
    const proc = Bun.spawn(["bflow", ...args], { cwd: directory, stdin: "pipe", stdout: "ignore", stderr: "pipe" })
    proc.stdin.write(JSON.stringify(input))
    proc.stdin.end()
    return proc
  }

  return {
    event: async ({ event }) => {
      const p = event.properties ?? {}
      try {
        if (event.type === "session.created" || event.type === "session.updated") {
          note(p.info.id, { parentID: p.info.parentID ?? "", known: true })
        } else if (event.type === "message.updated" && p.info?.role === "assistant") {
          const i = p.info
          note(i.sessionID, { agent: i.agent ?? i.mode })
          if (!i.time?.completed || !existsSync(join(directory, ".bflow")) || !safe(i.sessionID)) return
          const s = await resolve(i.sessionID)
          const dir = join(directory, ".bflow", "cache", "opencode")
          mkdirSync(dir, { recursive: true })
          const line = { id: i.id, sessionID: i.sessionID, parentID: s.parentID ?? "", agent: s.agent ?? "", providerID: i.providerID,
            modelID: i.modelID, created: i.time.created, completed: i.time.completed, tokens: i.tokens }
          appendFileSync(join(dir, safe(i.sessionID) + ".jsonl"), JSON.stringify(line) + "\n")
        } else if (event.type === "session.idle") {
          const s = await resolve(p.sessionID)
          run(["hook", "tokens", "--tool", "opencode"], { sessionID: p.sessionID, parentID: s.parentID ?? "", cwd: directory })
        }
      } catch {} // un fallo del plugin nunca interrumpe a OpenCode
    },
    "tool.execute.before": async (input, output) => {
      if (!GUARDED.has(input.tool)) return
      if (!Bun.which("bflow")) return warn("no esta en el PATH: el guard no esta activo")
      let code = -1, err = "", timer
      try {
        const s = await resolve(input.sessionID)
        const sub = Boolean(s.parentID)
        const proc = run(["guard", "--tool", "opencode"], { tool: input.tool, args: output.args, sessionID: input.sessionID,
          agent: sub ? s.agent ?? "" : "", subagent: sub, cwd: directory })
        timer = setTimeout(() => proc.kill(), 10000)
        err = await new Response(proc.stderr).text()
        code = await proc.exited
      } catch {} finally {
        clearTimeout(timer)
      }
      if (code === 2) throw new Error(err.trim() || "bloqueado por bflow guard")
      if (code !== 0) warn("el guard no pudo correr (codigo " + code + "): no esta activo")
    },
  }
}
