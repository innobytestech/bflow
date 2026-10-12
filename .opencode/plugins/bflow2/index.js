// <!-- generado por bflow render: no lo edites; cambia bflow.yaml (agents) o el archivo extra y vuelve a correrlo -->
// Plugin de bflow para OpenCode 2.x: guard antes de cada herramienta, tokens por sesion e inyeccion del estado.
// Solo le pasa datos a bflow (por stdin); toda regla vive en `bflow guard`.
import { appendFileSync, existsSync, mkdirSync } from "node:fs"
import { join } from "node:path"

const GUARDED = new Set(["shell", "edit", "write", "patch", "read", "subagent"])
const safe = (id) => String(id ?? "").replace(/[^A-Za-z0-9_-]/g, "")

export default {
  id: "bflow",
  setup: async (ctx) => {
    const dir = ctx.location.directory
    const sessions = new Map() // sessionID -> { parentID, known }
    const steps = new Map() // assistantMessageID -> { providerID, modelID, agent, started }
    const injected = new Set()
    let warned = false, stop = false
    const warn = (msg) => {
      if (warned) return
      warned = true
      console.error("bflow: " + msg)
    }
    const resolve = async (sid) => {
      if (!sessions.get(sid)?.known) {
        try {
          const r = await ctx.session.get({ sessionID: sid })
          sessions.set(sid, { parentID: r?.parentID ?? r?.data?.parentID ?? "", known: true })
        } catch {}
      }
      return sessions.get(sid) ?? {}
    }
    const run = (args, input, wait = true) => { // wait=false: no se lee stderr ni se espera al proceso
      const proc = Bun.spawn(["bflow", ...args], { cwd: dir, stdin: "pipe", stdout: "ignore", stderr: wait ? "pipe" : "ignore" })
      proc.stdin.write(JSON.stringify(input))
      proc.stdin.end()
      return proc
    }

    ctx.tool.hook("execute.before", async (x) => {
      if (!GUARDED.has(x.tool)) return
      if (!Bun.which("bflow")) return warn("no esta en el PATH: el guard no esta activo")
      let code = -1, err = "", timer
      try {
        const s = await resolve(x.sessionID)
        const sub = Boolean(s.parentID)
        const body = { tool: x.tool, args: x.input, sessionID: x.sessionID, agent: sub ? x.agent ?? "" : "", subagent: sub, cwd: dir }
        if (x.tool === "subagent" || (x.tool === "read" && !(sub && x.agent === "bflow-reviewer"))) { // subagent y el read ajeno solo se registran
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
    })

    // Primer prompt de una sesion raiz: el estado de bflow entra al contexto como texto sintetico.
    ctx.session.hook("prompt", async (x) => {
      try {
        const sid = x.sessionID
        if (injected.has(sid) || !Bun.which("bflow") || !existsSync(join(dir, ".bflow"))) return
        if ((await resolve(sid)).parentID) return
        injected.add(sid)
        const proc = Bun.spawn(["bflow", "hook", "session-start"], { cwd: dir, stdin: "pipe", stdout: "pipe", stderr: "ignore" })
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
        if (code === 0 && text) await ctx.session.synthetic({ sessionID: sid, text })
      } catch {} // un fallo del plugin nunca interrumpe a OpenCode
    })

    const it = ctx.event.subscribe()
    ;(async () => {
      for await (const ev of it) {
        if (stop) break
        try {
          const d = ev.data ?? {}
          if (ev.type === "session.created") {
            sessions.set(d.sessionID, { parentID: d.parentID ?? "", known: true })
          } else if (ev.type === "session.step.started") {
            steps.set(d.assistantMessageID, { providerID: d.model?.providerID, modelID: d.model?.id, agent: d.agent, started: d.started })
          } else if (ev.type === "session.step.ended" || ev.type === "session.step.failed") {
            if (!existsSync(join(dir, ".bflow")) || !safe(d.sessionID)) continue
            if (ev.type === "session.step.failed" && !d.tokens) continue
            const st = steps.get(d.assistantMessageID) ?? {}
            steps.delete(d.assistantMessageID)
            const s = await resolve(d.sessionID)
            const cache = join(dir, ".bflow", "cache", "opencode")
            mkdirSync(cache, { recursive: true })
            const line = { id: ev.id, sessionID: d.sessionID, parentID: s.parentID ?? "", agent: st.agent ?? "", providerID: st.providerID,
              modelID: st.modelID, created: st.started ?? ev.created, completed: ev.created, tokens: d.tokens }
            appendFileSync(join(cache, safe(d.sessionID) + ".jsonl"), JSON.stringify(line) + "\n")
          } else if (ev.type === "session.execution.succeeded" || ev.type === "session.execution.failed" || ev.type === "session.execution.interrupted") {
            const s = await resolve(d.sessionID)
            run(["hook", "tokens", "--tool", "opencode"], { sessionID: d.sessionID, parentID: s.parentID ?? "", cwd: dir }, false)
          }
        } catch {}
      }
    })().catch(() => {})
    return () => { stop = true; it.return?.() }
  },
}
