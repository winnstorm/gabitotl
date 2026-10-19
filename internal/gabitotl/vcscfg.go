package gabitotl

import (
	"errors"
	"fmt"
	"io/fs"
)

// Delivery settings (commit, push, PR, merge) are split in two files, the same
// way the memory settings are (docs/DECISIONS.md D30, D33):
//
//   - .gabitotl/vcs.md (committed): what the team agreed on. Provider, what is
//     automatic, merge method, branch deletion, draft PRs, base branch, the PR size
//     budget, the chain strategy and the `configured` marker.
//   - .gabitotl/local/users/<id>/vcs.md (never committed): the auth reference and
//     optional permission overrides, for a person who wants to be more careful
//     than the team. The local value wins.
//
// VCSConfig is the merged view, so callers keep reading one struct. A legacy
// vcs.md that still carries `auth` keeps working and moves on the next write.

// Automation levels the delivery question offers, plus the label of anything else.
const (
	AutomationAll     = "all"      // commit, push, PR and merge
	AutomationUntilPR = "until-pr" // commit, push and PR; the person merges
	AutomationAsk     = "ask"      // confirm every operation
	AutomationCustom  = "custom"   // permissions edited by hand
)

// AutomationLevels lists the values the questions and flags accept.
var AutomationLevels = []string{AutomationAll, AutomationUntilPR, AutomationAsk}

// Targets of SaveDelivery: the committed file or this person's own file.
const (
	TargetShared = "shared"
	TargetYours  = "yours"
)

// permKeys are the permission names as they appear in the files.
var permKeys = []string{"branches", "commits", "push", "pull_requests", "merge"}

// localPerms are the per-person permission overrides; an empty value is "no override".
type localPerms struct {
	Branches     string `yaml:"branches,omitempty"`
	Commits      string `yaml:"commits,omitempty"`
	Push         string `yaml:"push,omitempty"`
	PullRequests string `yaml:"pull_requests,omitempty"`
	Merge        string `yaml:"merge,omitempty"`
}

// localVCS is the front matter of the per-person vcs.md.
type localVCS struct {
	Auth        string     `yaml:"auth,omitempty"`
	Permissions localPerms `yaml:"permissions,omitempty"`
}

func (p localPerms) empty() bool { return p == localPerms{} }

// permFields pairs every permission key with its address in a VCSPermissions.
func permFields(p *VCSPermissions) map[string]*string {
	return map[string]*string{
		"branches": &p.Branches, "commits": &p.Commits, "push": &p.Push,
		"pull_requests": &p.PullRequests, "merge": &p.Merge,
	}
}

func localFields(p *localPerms) map[string]*string {
	return map[string]*string{
		"branches": &p.Branches, "commits": &p.Commits, "push": &p.Push,
		"pull_requests": &p.PullRequests, "merge": &p.Merge,
	}
}

func (l localVCS) validate() error {
	if l.Auth != "" && !IsSafeRef(l.Auth) {
		return fmt.Errorf("auth: must be a reference (native, env:NAME or keychain:NAME)")
	}
	lp := l.Permissions
	for _, k := range permKeys {
		if v := *localFields(&lp)[k]; v != "" {
			if err := oneOf("permissions."+k, v, PermAsk, PermAllow, PermDeny); err != nil {
				return err
			}
		}
	}
	return nil
}

// readLocalVCS returns the per-person settings; ok is false when the file does not
// exist (or has no front matter).
func readLocalVCS(l Layout, user string) (loc localVCS, ok bool, err error) {
	p, err := l.LocalVCS(user)
	if err != nil {
		return loc, false, err
	}
	d, err := ReadDoc(p)
	if errors.Is(err, fs.ErrNotExist) {
		return loc, false, nil
	}
	if err != nil {
		return loc, false, err
	}
	if !d.HasFront {
		return loc, false, nil
	}
	if err := d.Decode(&loc); err != nil {
		return loc, false, fmt.Errorf("%s: %w", p, err)
	}
	if err := loc.validate(); err != nil {
		return loc, false, fmt.Errorf("%s: %w", p, err)
	}
	return loc, true, nil
}

func writeLocalVCS(l Layout, user string, loc localVCS) error {
	p, err := l.LocalVCS(user)
	if err != nil {
		return err
	}
	if _, err := EnsureLocalUserDir(l, user); err != nil {
		return err
	}
	body := bodyLocalVCS
	if d, err := ReadDoc(p); err == nil {
		body = d.Body
	}
	return WriteDoc(p, loc, body)
}

// readSharedVCS decodes the committed vcs.md over the defaults.
func readSharedVCS(l Layout) (VCSConfig, error) { return readSharedVCSOver(l, DefaultVCS("git")) }

// readSharedVCSOver decodes the committed vcs.md over base, so a key the file
// does not carry keeps the value of base (the global defaults).
func readSharedVCSOver(l Layout, base VCSConfig) (VCSConfig, error) {
	c := base
	return c, readConfig(l.VCS(), &c)
}

// LoadVCS returns the merged settings of the active person (see ActiveUserID).
func LoadVCS(l Layout) (VCSConfig, error) { return LoadVCSFor(l, ActiveUserID()) }

// LoadVCSFor merges the shared vcs.md with the per-person file of user; the local
// value wins per field. A shared file that still carries `auth` (legacy) is the
// fallback when the local file has none. An empty or invalid user reads the shared
// file alone. A missing shared file is reported as fs.ErrNotExist, as before.
func LoadVCSFor(l Layout, user string) (VCSConfig, error) {
	return LoadVCSLayered(l, user, DefaultVCS("git"))
}

// LoadVCSLayered is LoadVCSFor with base as the lowest layer instead of the
// built-in defaults. Callers pass the global defaults (LoadGlobalVCS), which gives
// the order: built-in default, global user file, project team file, per-person file.
func LoadVCSLayered(l Layout, user string, base VCSConfig) (VCSConfig, error) {
	c, err := readSharedVCSOver(l, base)
	if err != nil || !ValidID(user) {
		return c, err
	}
	loc, ok, err := readLocalVCS(l, user)
	if err != nil || !ok {
		return c, err
	}
	if loc.Auth != "" {
		c.Auth = loc.Auth
	}
	lp := loc.Permissions
	dst := permFields(&c.Permissions)
	for k, src := range localFields(&lp) {
		if *src != "" {
			*dst[k] = *src
		}
	}
	return c, c.Validate()
}

// SaveVCS splits c between the shared and the per-person file of the active
// person (see SaveVCSFor).
func SaveVCS(l Layout, c VCSConfig) error { return SaveVCSFor(l, ActiveUserID(), c) }

// SaveVCSFor writes c, the merged view, back to the two files. Everything goes to
// the shared file except `auth`, which moves to the per-person file (and leaves
// the shared one). A permission the person overrides locally keeps its shared
// value when c still carries the override; a different value is a deliberate
// change, so it replaces the shared value and drops the override. With no usable
// user id everything stays in the shared file, as before the split.
func SaveVCSFor(l Layout, user string, c VCSConfig) error {
	if err := c.Validate(); err != nil {
		return err
	}
	if !ValidID(user) {
		return saveConfig(l.VCS(), &c, bodyVCS)
	}
	loc, hadLocal, err := readLocalVCS(l, user)
	if err != nil {
		return err
	}
	cur, err := readSharedVCS(l)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	shared := c
	shared.Auth = ""
	sp, cp, curp := permFields(&shared.Permissions), permFields(&c.Permissions), permFields(&cur.Permissions)
	lp := loc.Permissions
	for k, ov := range localFields(&lp) {
		if *ov == "" {
			continue
		}
		if *cp[k] == *ov {
			*sp[k] = *curp[k] // still the override: the team value is not touched
		} else {
			*ov = "" // changed on purpose: the new value is the team's now
		}
	}
	loc.Permissions = lp
	loc.Auth = c.Auth
	if hadLocal || loc.Auth != "" || !loc.Permissions.empty() {
		if err := writeLocalVCS(l, user, loc); err != nil { // local first: the shared file may still hold the legacy auth
			return err
		}
	}
	return saveConfig(l.VCS(), &shared, bodyVCS)
}

// VCSOrigin tells where the delivery settings of the active person come from.
type VCSOrigin struct {
	SharedPath string
	// LocalPath is the per-person file ("" when no id is available) and HasLocal
	// whether it exists.
	LocalPath string
	HasLocal  bool
	// Overrides lists the permissions this person overrides (file keys, for example
	// "merge"). AuthFromLocal says auth comes from the per-person file; LegacyAuth
	// that the committed file still carries one (it moves on the next write).
	Overrides     []string
	AuthFromLocal bool
	LegacyAuth    bool
}

// InspectVCS reports the origin of the settings; it never fails (an unreadable
// file reads as empty, LoadVCS is what reports it).
func InspectVCS(l Layout, user string) VCSOrigin {
	o := VCSOrigin{SharedPath: l.VCS()}
	if sh, err := readSharedVCS(l); err == nil {
		o.LegacyAuth = sh.Auth != ""
	}
	if !ValidID(user) {
		return o
	}
	if p, err := l.LocalVCS(user); err == nil {
		o.LocalPath = p
	}
	loc, ok, err := readLocalVCS(l, user)
	if err != nil || !ok {
		return o
	}
	o.HasLocal = true
	o.AuthFromLocal = loc.Auth != ""
	lp := loc.Permissions
	for _, k := range permKeys {
		if *localFields(&lp)[k] != "" {
			o.Overrides = append(o.Overrides, k)
		}
	}
	return o
}

// AutomationLevel names what the permissions amount to: all (commit, push, PR and
// merge), until-pr (merge stays with the person), ask, or custom.
func (c VCSConfig) AutomationLevel() string {
	p := c.Permissions
	upToPR := p.Branches == PermAllow && p.Commits == PermAllow && p.Push == PermAllow && p.PullRequests == PermAllow
	switch {
	case upToPR && p.MergePermission() == PermAllow:
		return AutomationAll
	case upToPR && p.MergePermission() == PermAsk:
		return AutomationUntilPR
	}
	for _, v := range []string{p.Branches, p.Commits, p.Push, p.PullRequests, p.MergePermission()} {
		if v != PermAsk {
			return AutomationCustom
		}
	}
	return AutomationAsk
}

// DeliveryAnswers are the answers to the delivery questions (or their flags).
// An empty field is "not answered": the current value is kept.
type DeliveryAnswers struct {
	Provider     string // github | gitlab | bitbucket | ... (a VCS provider)
	Automation   string // all | until-pr | ask
	MergeMethod  string
	DeleteBranch *bool
	Draft        *bool
	// Shared forces the write into the team file even when the project is already
	// configured and only the automation was answered.
	Shared bool
}

// Empty reports that nothing was answered.
func (a DeliveryAnswers) Empty() bool {
	return a.Provider == "" && a.Automation == "" && a.MergeMethod == "" && a.DeleteBranch == nil && a.Draft == nil
}

// TeamLevel reports whether a team-wide setting (not just the person's automation) was answered.
func (a DeliveryAnswers) TeamLevel() bool {
	return a.Provider != "" || a.MergeMethod != "" || a.DeleteBranch != nil || a.Draft != nil
}

// Validate rejects values outside the allowed sets.
func (a DeliveryAnswers) Validate() error {
	if a.Provider != "" {
		if err := oneOf("vcs provider", a.Provider, VCSProviders...); err != nil {
			return err
		}
	}
	if a.Automation != "" {
		if err := oneOf("vcs automation", a.Automation, AutomationLevels...); err != nil {
			return err
		}
	}
	if a.MergeMethod != "" {
		return oneOf("vcs merge method", a.MergeMethod, MergeMethods...)
	}
	return nil
}

// permissionsFor returns the permissions the automation level stands for.
func permissionsFor(automation string) (VCSPermissions, bool) {
	switch automation {
	case AutomationAll:
		return VCSPermissions{PermAllow, PermAllow, PermAllow, PermAllow, PermAllow}, true
	case AutomationUntilPR:
		return VCSPermissions{PermAllow, PermAllow, PermAllow, PermAllow, PermAsk}, true
	case AutomationAsk:
		return VCSPermissions{PermAsk, PermAsk, PermAsk, PermAsk, PermAsk}, true
	}
	return VCSPermissions{}, false
}

// applyDelivery writes the answers into c. Answering the automation settles the delivery
// questions, so the project counts as configured.
func (c *VCSConfig) applyDelivery(a DeliveryAnswers) {
	if a.Provider != "" {
		c.Provider = a.Provider
	}
	if p, ok := permissionsFor(a.Automation); ok {
		c.Permissions = p
		c.RemoteActions = "ask"
		if a.Automation != AutomationAsk {
			c.RemoteActions = "automatic"
		}
		c.Configured = true
	}
	if a.MergeMethod != "" {
		c.MergeMethod = a.MergeMethod
	}
	if a.DeleteBranch != nil {
		c.DeleteBranch = *a.DeleteBranch
	}
	if a.Draft != nil {
		c.DraftPRs = *a.Draft
	}
}

// DeliveryResult says where SaveDelivery wrote.
type DeliveryResult struct {
	Target string    // TargetShared or TargetYours
	Path   string    // the file written
	Config VCSConfig // the merged settings afterwards
}

// SaveDelivery stores the answers. The rule, in one place:
//
//   - The team file is written when it is new or not configured yet, when the
//     answers include a team-wide setting (provider, merge method, branch deletion,
//     draft PRs), when Shared is set, or when there is no usable user id.
//   - Otherwise the project is already configured and the person only chose how
//     much to automate: that goes to the per-person file as permission overrides
//     (none when it equals the team's), so one person never changes it for others.
//
// Writing the team file also drops this person's own overrides for the permissions
// it sets, so the answer is not silently contradicted.
func SaveDelivery(l Layout, user string, a DeliveryAnswers) (DeliveryResult, error) {
	if err := a.Validate(); err != nil {
		return DeliveryResult{}, err
	}
	sh, err := readSharedVCS(l)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return DeliveryResult{}, err
	}
	toShared := !ValidID(user) || a.Shared || a.TeamLevel() || !sh.IsConfigured()
	if toShared {
		sh.applyDelivery(a)
		if err := writeSharedVCS(l, user, sh, a.Automation != ""); err != nil {
			return DeliveryResult{}, err
		}
		cfg, err := LoadVCSFor(l, user)
		return DeliveryResult{Target: TargetShared, Path: l.VCS(), Config: cfg}, err
	}
	loc, hadLocal, err := readLocalVCS(l, user)
	if err != nil {
		return DeliveryResult{}, err
	}
	want, ok := permissionsFor(a.Automation)
	if !ok {
		cfg, err := LoadVCSFor(l, user)
		return DeliveryResult{Target: TargetYours, Config: cfg}, err
	}
	var ov localPerms
	wp, sp, lp := permFields(&want), permFields(&sh.Permissions), localFields(&ov)
	for _, k := range permKeys {
		cur := *sp[k]
		if k == "merge" {
			cur = sh.Permissions.MergePermission()
		}
		if *wp[k] != cur {
			*lp[k] = *wp[k]
		}
	}
	loc.Permissions = ov
	if hadLocal || !ov.empty() {
		if err := writeLocalVCS(l, user, loc); err != nil {
			return DeliveryResult{}, err
		}
	}
	p, _ := l.LocalVCS(user)
	cfg, err := LoadVCSFor(l, user)
	return DeliveryResult{Target: TargetYours, Path: p, Config: cfg}, err
}

// writeSharedVCS writes the team file. A legacy `auth` in it moves to the
// per-person file first; dropOverrides also clears this person's permission
// overrides.
func writeSharedVCS(l Layout, user string, sh VCSConfig, dropOverrides bool) error {
	if ValidID(user) {
		loc, hadLocal, err := readLocalVCS(l, user)
		if err != nil {
			return err
		}
		changed := false
		if sh.Auth != "" {
			if loc.Auth == "" {
				loc.Auth = sh.Auth
			}
			sh.Auth, changed = "", true
		}
		if dropOverrides && !loc.Permissions.empty() {
			loc.Permissions, changed = localPerms{}, true
		}
		if changed && (hadLocal || loc.Auth != "" || !loc.Permissions.empty()) {
			if err := writeLocalVCS(l, user, loc); err != nil {
				return err
			}
		}
	}
	return saveConfig(l.VCS(), &sh, bodyVCS)
}
