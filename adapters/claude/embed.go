package claude

import _ "embed"

// Skill es la skill del leader, fuente única de bflow install claude.
//
//go:embed skills/bflow/SKILL.md
var Skill []byte

// Settings son los ajustes de bflow (hooks, statusLine, permisos) que install
// fusiona en .claude/settings.json del repo.
//
//go:embed settings.json
var Settings []byte
