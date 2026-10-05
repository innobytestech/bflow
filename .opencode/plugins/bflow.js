// <!-- generado por bflow render: no lo edites; cambia bflow.yaml (agents) o el archivo extra y vuelve a correrlo -->
// Plugin de bflow para OpenCode: guard antes de cada herramienta y tokens por sesion.
// Solo le pasa datos a bflow (por stdin); toda regla vive en `bflow guard`.
import { appendFileSync, existsSync, mkdirSync } from "node:fs"
import { join } from "node:path"

const GUARDED = new Set(["bash", "edit", "write", "multiedit", "patch", "apply_patch", "read", "task"])
const safe = (id) => String(id ?? "").replace(/[^A-Za-z0-9_-]/g, "")

export const BflowPlugin = async ({ client, directory }) => {
  const sessions = new Map() // sessionID -> { parentID, agent }
  let warned = false
  const warn = (msg) => {
    if (warned) return
    warned = true
    const text = "bflow: " + msg
    Promise.resolve(client?.tui?.showToast?.({ body: { message: text, variant: "warning" } }))
      .then((r) => { if (!r || r.error) throw r?.error })
      .catch(() => console.error(text)) // sin TUI (opencode run) el aviso sale por stderr
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
  const run = (args, input, wait = true) => { // wait=false: no se lee stderr ni se espera al proceso
    const proc = Bun.spawn(["bflow", ...args], { cwd: directory, stdin: "pipe", stdout: "ignore", stderr: wait ? "pipe" : "ignore" })
    proc.stdin.write(JSON.stringify(input))
    proc.stdin.end()
    return proc
  }

  // Sesion raiz nueva: el estado de bflow entra al contexto sin pedirle respuesta al modelo.
  const inject = async (id) => {
    if (!Bun.which("bflow") || !existsSync(join(directory, ".bflow"))) return
    const proc = Bun.spawn(["bflow", "hook", "session-start"], { cwd: directory, stdin: "pipe", stdout: "pipe", stderr: "ignore" })
    proc.stdin.write("{}")
    proc.stdin.end()
    const timer = setTimeout(() => proc.kill(), 10000)
    let text = "", code = -1
    try {
      text = (await new Response(proc.stdout).text()).trim()
      code = await proc.exited
    } finally {
      clearTimeout(timer)
    }
    if (code !== 0 || !text) return
    await client.session.prompt({ path: { id }, body: { noReply: true, parts: [{ type: "text", text }] } })
  }

  return {
    event: async ({ event }) => {
      const p = event.properties ?? {}
      try {
        if (event.type === "session.created" || event.type === "session.updated") {
          note(p.info.id, { parentID: p.info.parentID ?? "", known: true })
          if (event.type === "session.created" && !p.info.parentID) await inject(p.info.id)
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
        const body = { tool: input.tool, args: output.args, sessionID: input.sessionID, agent: sub ? s.agent ?? "" : "", subagent: sub, cwd: directory }
        if (input.tool === "task" || (input.tool === "read" && !(sub && s.agent === "bflow-reviewer"))) { // task y el read ajeno solo se registran
          run(["guard", "--tool", "opencode", "--reads"], body, false); return }
        const proc = run(["guard", "--tool", "opencode", "--reads"], body)
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
