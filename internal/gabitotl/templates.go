package gabitotl

// Default Markdown bodies (the text after the front matter). Bodies start
// with a blank line so the rendered file reads naturally after the closing fence.
const (
	bodyProject = `
# Project

Describe what this project is, who it is for and the problem it solves.

## Goal

...

## Known constraints

...
`
	bodyMemory = `
# Memory

Project memory lives in the configured provider. This file only holds
references: never put passwords, tokens or connection strings here. Use
` + "`env:NAME`" + `, ` + "`keychain:NAME`" + ` or ` + "`native`" + `.
`
	bodyLocalMemory = `
# Memory (this machine)

Your own team-memory settings: role, token variable, database reference and an
optional local server url. This file is never committed. Only references live
here (` + "`env:NAME`" + `), never a token or a password.
`
	bodyRuntime = `
# Runtime

Default runtime and model for this project.
`
	bodyVCS = `
# Version control

Validated changes are committed per logical unit. Remote actions follow the
permissions above.
`
	bodyLocalVCS = `
# Version control (this machine)

Your own delivery settings: the auth reference and, if you want to be more
careful than the team, permission overrides. This file is never committed. Only
references live here (` + "`env:NAME`" + `, ` + "`native`" + `), never a token or a password.
`
	bodyWorkflow = `
# Workflow

Gabito works as a senior tech lead and keeps answers short.
`
	bodyImports = `
# Imports

Records of material imported from other harnesses or tools. Imported content
is historical context and never overrides current code or this configuration.
`
	bodyUser = `
# Developer

Known developer of this project. No emails or secrets are stored here.
`
)

const importsFront = "imports: []\n"
