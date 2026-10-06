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
	bodyRuntime = `
# Runtime

Default runtime and model for this project.
`
	bodyVCS = `
# Version control

Validated changes are committed per logical unit. Remote actions follow the
permissions above.
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
