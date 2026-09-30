# GH-13 implementación

Cobertura por R (pruebas del contrato, sin cambios):
- R1-R4: config TestToolsYAML, TestAgentListValidate, TestModelsMerge, TestModelsValidate.
- R5, R7-R10: TestOpenCodeRender, TestOpenCodeGenerated (adaptador); TestRenderMultiTool, TestRenderStaleInactiveTool, TestRenderEmptyAgentClaudeOnly (cli).
- R6: TestResolveModels; TestRenderUnresolved (data.unresolved y `aviso:`).
- R11-R13: TestLeaderRender, TestSkillFromCommonBody (sha256 de antes), TestCommandFromCommonBody.
- R14-R16: TestOpenCodeInstallXDG, TestOpenCodeInstallBadDir, TestInstallOpenCode, TestInstallOpenCodeNotInAgent.
- R17: TestUpdateRefreshesIntegrations (ver NEEDS_DECISION).
- R18-R22: TestInitDetectsOpenCode, TestInitAgentFlagList, TestRenderYAMLAgentList, TestDoctorOpenCode, TestDoctorOpenCodeOnly, TestOpenCodeSkillStateCRLF.
- R23: docs sin prueba (README, docs/guia.md, adapters/opencode/README.md).

Mutaciones (prueba · mutación · falló · restaurado):
- TestOpenCodeGenerated · aceptar cualquier bflow-*.md sin marca · sí · sí
- TestOpenCodeInstallBadDir · aceptar XDG_CONFIG_HOME relativa · sí · sí
- TestResolveModels · no respetar "/" en el modelo · sí · sí
- TestRenderStaleInactiveTool · no consultar GeneratedAgents de todas las herramientas · sí · sí

Decisiones:
- Mecánicas: `ask_tool` de OpenCode es "la herramienta `question`" (del contrato); el texto de update para el comando dice "comando de opencode actualizado".
- `data.unresolved` va siempre (lista vacía si no hay), ordenada por herramienta registrada y alias.
- doctor: `agentes` sale del bloque de claude y corre una vez si `agent:` no está vacío; en OpenCode sin home usa XDG si existe.
- Seguridad: escritura solo en .opencode/agents/bflow-*.md y el comando del usuario; borrado solo con marca; frontmatter con yaml.Marshal.

Bloqueo:
- TestUpdateRefreshesIntegrations/falla_el_refresco lee `out["text"]` pero el JSON de bflow no serializa Text (`json:"-"`); nunca puede pasar. El comportamiento sí está (verificado con una prueba temporal en modo texto: "corre bflow install claude" y "corre bflow install opencode").
