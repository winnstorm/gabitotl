package gui

import "errors"

// PlanDesktop shows what adding the given Windows integration pieces (install
// folder, Start menu entry, desktop shortcut, PATH) would do. It writes nothing.
// The Manager uses it to add a piece that is missing after the first install.
func (s *Service) PlanDesktop(w WindowsAnswers) (Plan, error) {
	steps, supported, err := s.desktopSteps(s.desktopOptions(w))
	if err != nil {
		return Plan{}, err
	}
	p := Plan{Groups: []PlanGroup{}, Notes: []Note{}, Steps: []PlanStep{}, Supported: supported, Mutates: supported && len(steps) > 0}
	for _, st := range steps {
		p.Steps = append(p.Steps, PlanStep{ID: "desktop." + st.ID, Title: st.Title})
	}
	if !supported {
		p.Unsupports = []string{"desktop"}
	}
	return seal(p), nil
}

// ApplyDesktop applies the pieces PlanDesktop listed; planID must be the id it
// returned for the same answers.
func (s *Service) ApplyDesktop(w WindowsAnswers, planID string, p Progress) (StepResult, error) {
	fresh, err := s.PlanDesktop(w)
	if err != nil {
		return StepResult{}, err
	}
	if err := checkPlan(planID, fresh); err != nil {
		return StepResult{}, err
	}
	r := s.applyDesktop(s.desktopOptions(w), p, 10, 100)
	switch r.Status {
	case StatusUnsupported:
		return r, ErrUnsupportedOS
	case StatusError:
		return r, errors.New(r.Message)
	}
	p.emit("done", "done", "", 100)
	return r, nil
}
