package scanner

import (
	"encoding/json"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

func readSmall(p string) string {
	f, err := os.Open(p)
	if err != nil {
		return ""
	}
	defer f.Close()
	b, _ := io.ReadAll(io.LimitReader(f, maxManifestBytes))
	return string(b)
}

type stackBuilder struct {
	stack map[string]bool
	cmds  map[string]Command
	pm    *ProjectMap
}

func (s *stackBuilder) add(v string) { s.stack[v] = true }
func (s *stackBuilder) cmd(c, source string) {
	if _, ok := s.cmds[c]; !ok {
		s.cmds[c] = Command{Command: c, Source: source}
	}
}

var (
	goModuleRe = regexp.MustCompile(`(?m)^module\s+(\S+)`)
	goVersion  = regexp.MustCompile(`(?m)^go\s+(\S+)`)
	makeTarget = regexp.MustCompile(`^([A-Za-z0-9][A-Za-z0-9_.-]*)\s*:($|[^=])`)
)

// analyzeManifests turns manifest files into stack hints and known commands.
// Commands are reported only when declared (Makefile targets, package.json
// scripts) or implied by an unambiguous toolchain manifest.
func analyzeManifests(root string, manifests []string, pm *ProjectMap) {
	sort.Strings(manifests)
	pm.Manifests = manifests
	s := &stackBuilder{stack: map[string]bool{}, cmds: map[string]Command{}, pm: pm}
	for _, m := range manifests {
		abs := filepath.Join(root, m)
		dir := path.Dir(m)
		prefix := ""
		if dir != "." {
			prefix = "cd " + dir + " && "
		}
		switch path.Base(m) {
		case "go.mod":
			txt := readSmall(abs)
			lang := "Go"
			if v := goVersion.FindStringSubmatch(txt); v != nil {
				lang += " " + v[1]
			}
			s.add(lang)
			if mod := goModuleRe.FindStringSubmatch(txt); mod != nil && dir == "." {
				s.add("Go module " + mod[1])
			}
			s.cmd(prefix+"go build ./...", m)
			s.cmd(prefix+"go test ./...", m)
		case "package.json":
			s.packageJSON(abs, m, prefix)
		case "pyproject.toml", "requirements.txt", "setup.py":
			txt := strings.ToLower(readSmall(abs))
			s.add("Python")
			for _, fw := range []string{"django", "flask", "fastapi", "pytest", "sqlalchemy", "celery"} {
				if strings.Contains(txt, fw) {
					s.add(fw)
				}
			}
			if strings.Contains(txt, "pytest") {
				s.cmd(prefix+"pytest", m)
			}
		case "pom.xml":
			s.add("Java (Maven)")
			s.cmd(prefix+"mvn test", m)
		case "build.gradle", "build.gradle.kts":
			s.add("JVM (Gradle)")
			s.cmd(prefix+"gradle test", m)
		case "Cargo.toml":
			s.add("Rust")
			s.cmd(prefix+"cargo build", m)
			s.cmd(prefix+"cargo test", m)
		case "composer.json":
			s.add("PHP")
			s.composer(abs, m, prefix)
		case "Gemfile":
			s.add("Ruby")
		case "mix.exs":
			s.add("Elixir")
		case "Makefile":
			for _, ln := range strings.Split(readSmall(abs), "\n") {
				if mm := makeTarget.FindStringSubmatch(ln); mm != nil && !strings.HasPrefix(mm[1], ".") {
					s.cmd(prefix+"make "+mm[1], m)
				}
			}
		case "Dockerfile", "docker-compose.yml", "docker-compose.yaml", "compose.yaml":
			pm.Integrations = appendUnique(pm.Integrations, "Docker")
		}
	}
	for v := range s.stack {
		pm.Stack = append(pm.Stack, v)
	}
	sort.Strings(pm.Stack)
	for _, c := range s.cmds {
		pm.Commands = append(pm.Commands, c)
	}
	sort.Slice(pm.Commands, func(i, j int) bool {
		if pm.Commands[i].Source != pm.Commands[j].Source {
			return pm.Commands[i].Source < pm.Commands[j].Source
		}
		return pm.Commands[i].Command < pm.Commands[j].Command
	})
}

func (s *stackBuilder) packageJSON(abs, m, prefix string) {
	var pj struct {
		Name         string            `json:"name"`
		Description  string            `json:"description"`
		Scripts      map[string]string `json:"scripts"`
		Dependencies map[string]string `json:"dependencies"`
		DevDeps      map[string]string `json:"devDependencies"`
	}
	if json.Unmarshal([]byte(readSmall(abs)), &pj) != nil {
		s.add("Node.js")
		return
	}
	s.add("Node.js")
	if s.pm.Purpose == "" && path.Dir(m) == "." {
		s.pm.Purpose = strings.TrimSpace(pj.Description)
	}
	deps := map[string]bool{}
	for k := range pj.Dependencies {
		deps[k] = true
	}
	for k := range pj.DevDeps {
		deps[k] = true
	}
	for dep, label := range map[string]string{
		"typescript": "TypeScript", "react": "React", "next": "Next.js", "vue": "Vue", "nuxt": "Nuxt",
		"svelte": "Svelte", "express": "Express", "@nestjs/core": "NestJS", "vite": "Vite",
		"jest": "Jest", "vitest": "Vitest", "prisma": "Prisma",
	} {
		if deps[dep] {
			s.add(label)
		}
	}
	names := make([]string, 0, len(pj.Scripts))
	for n := range pj.Scripts {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		s.cmd(prefix+"npm run "+n, m)
	}
}

func (s *stackBuilder) composer(abs, m, prefix string) {
	var c struct {
		Scripts map[string]json.RawMessage `json:"scripts"`
	}
	if json.Unmarshal([]byte(readSmall(abs)), &c) != nil {
		return
	}
	for n := range c.Scripts {
		if !strings.HasPrefix(n, "post-") && !strings.HasPrefix(n, "pre-") {
			s.cmd(prefix+"composer run "+n, m)
		}
	}
}

func appendUnique(list []string, v string) []string {
	for _, x := range list {
		if x == v {
			return list
		}
	}
	return append(list, v)
}

var conventionalRe = regexp.MustCompile(`^[a-z]+(\([^)]+\))?!?: `)

// derive fills the interpretive sections from the observed layers. Everything
// stated here is observed in the repository; what cannot be inferred is listed
// under OpenQuestions instead of being guessed.
func derive(pm *ProjectMap, w *walk) {
	dirSet := map[string]bool{}
	for _, d := range pm.TopDirs {
		dirSet[d.Name] = true
	}
	// purpose guess: first prose paragraph line of the README.
	if pm.Purpose == "" {
		for _, d := range pm.Docs {
			if d.Excerpt != "" {
				pm.Purpose = proseLine(d.Excerpt)
				if pm.Purpose != "" {
					break
				}
			}
		}
	}
	// architecture observed
	if dirSet["cmd"] && dirSet["internal"] {
		pm.Architecture = append(pm.Architecture, "Go layout: cmd/ entrypoints with internal/ packages")
	}
	for _, d := range []string{"src", "app", "pkg", "services", "packages", "apps", "lib"} {
		if dirSet[d] {
			pm.Architecture = append(pm.Architecture, "Top-level source directory: "+d+"/")
		}
	}
	if dirSet["tests"] || dirSet["test"] || w.hasTests {
		pm.Architecture = append(pm.Architecture, "Automated tests present")
	}
	if dirSet["docs"] {
		pm.Architecture = append(pm.Architecture, "Documentation in docs/")
	}
	// conventions
	for flag, text := range map[string]string{
		".editorconfig": "EditorConfig", ".golangci.yml": "golangci-lint", ".golangci.yaml": "golangci-lint",
		".prettierrc": "Prettier", ".eslintrc": "ESLint", ".eslintrc.json": "ESLint", ".eslintrc.js": "ESLint",
		"eslint.config.js": "ESLint", "eslint.config.mjs": "ESLint", "ruff.toml": "Ruff",
		".pre-commit-config.yaml": "pre-commit hooks", "tsconfig.json": "TypeScript config",
		"contributing.md": "CONTRIBUTING.md present", "claude.md": "CLAUDE.md present", "agents.md": "AGENTS.md present",
	} {
		if w.flags[flag] {
			pm.Conventions = appendUnique(pm.Conventions, text)
		}
	}
	if n := len(pm.Git.Commits); n > 0 {
		c := 0
		for _, cm := range pm.Git.Commits {
			if conventionalRe.MatchString(cm.Subject) {
				c++
			}
		}
		if c*10 >= n*6 {
			pm.Conventions = append(pm.Conventions, "Conventional Commits (recent history)")
		}
	}
	sort.Strings(pm.Conventions)
	// integrations
	if w.flags["github-actions"] {
		pm.Integrations = appendUnique(pm.Integrations, "GitHub Actions")
	}
	for f, n := range map[string]string{".gitlab-ci.yml": "GitLab CI", "jenkinsfile": "Jenkins", "bitbucket-pipelines.yml": "Bitbucket Pipelines", "terraform": "Terraform"} {
		if w.flags[f] {
			pm.Integrations = appendUnique(pm.Integrations, n)
		}
	}
	sort.Strings(pm.Integrations)
	// risks
	if w.truncated {
		pm.Risks = append(pm.Risks, "Scan truncated at the file cap: structure counts are partial")
	}
	if w.flags[".env"] {
		pm.Risks = append(pm.Risks, "A .env file exists in the repository root: verify it holds no committed secrets")
	}
	if !w.hasTests && pm.FilesVisited > 0 {
		pm.Risks = append(pm.Risks, "No test files detected")
	}
	if len(pm.Integrations) == 0 && !w.flags["github-actions"] {
		pm.Risks = append(pm.Risks, "No CI configuration detected")
	}
	sort.Strings(pm.Risks)
	// open questions: what the scanner cannot infer
	pm.OpenQuestions = append(pm.OpenQuestions, "Business purpose, users and ownership are not inferable from the repository")
	if pm.Purpose == "" {
		pm.OpenQuestions = append(pm.OpenQuestions, "No README or package description: project purpose is unknown")
	}
	if len(pm.Stack) == 0 {
		pm.OpenQuestions = append(pm.OpenQuestions, "No recognized manifest: stack is unknown")
	}
	if len(pm.Commands) == 0 {
		pm.OpenQuestions = append(pm.OpenQuestions, "No build/test command detected: how is the project built and tested?")
	}
	if !pm.Git.IsRepo {
		pm.OpenQuestions = append(pm.OpenQuestions, "Not a Git repository (or git unavailable): history and branching model unknown")
	}
	pm.OpenQuestions = append(pm.OpenQuestions, "Deployment targets and runtime environments are unknown")
	// normalize nil slices so JSON output is stable ([] not null)
	for _, p := range []*[]string{&pm.Stack, &pm.Architecture, &pm.Conventions, &pm.Integrations, &pm.Risks, &pm.Manifests} {
		if *p == nil {
			*p = []string{}
		}
	}
	if pm.Commands == nil {
		pm.Commands = []Command{}
	}
	if pm.Docs == nil {
		pm.Docs = []Doc{}
	}
	if pm.Modules == nil {
		pm.Modules = []Module{}
	}
	if pm.TopDirs == nil {
		pm.TopDirs = []DirInfo{}
	}
	if pm.FileTypes == nil {
		pm.FileTypes = []ExtCount{}
	}
}

// proseLine returns the first non-heading, non-badge, non-code line.
func proseLine(excerpt string) string {
	inFence := false
	for _, ln := range strings.Split(excerpt, "\n") {
		t := strings.TrimSpace(ln)
		switch {
		case strings.HasPrefix(t, "```"):
			inFence = !inFence
		case inFence, t == "", strings.HasPrefix(t, "#"), strings.HasPrefix(t, "[!["), strings.HasPrefix(t, "!["), strings.HasPrefix(t, "<"), strings.HasPrefix(t, "---"):
		default:
			return t
		}
	}
	return ""
}
