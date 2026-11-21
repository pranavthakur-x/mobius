package orchestrator

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"

	"mobius/pkg/agent"
	"mobius/pkg/roles"
	"mobius/pkg/session"
	"mobius/pkg/tools"
)

// Manager owns teams, workers, and per-role claims for one workspace.
type Manager struct {
	mu      sync.Mutex
	teams   map[string]*Team
	order   []string
	claims  map[claimKey]struct{}
	store   *roles.Store
	factory AgentFactory
	model   ModelSpec
	base    *tools.Registry
}

func newManager(store *roles.Store, base *tools.Registry, model ModelSpec, factory AgentFactory) *Manager {
	return &Manager{
		teams:   make(map[string]*Team),
		order:   nil,
		claims:  make(map[claimKey]struct{}),
		store:   store,
		factory: factory,
		model:   model,
		base:    base,
	}
}

// CreateTeam provisions workers for a department id from the role store.
func (m *Manager) CreateTeam(deptID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.teams[deptID]; exists {
		return fmt.Errorf("team %q already exists", deptID)
	}
	dept, ok := m.store.Department(deptID)
	if !ok {
		return fmt.Errorf("unknown department %q", deptID)
	}
	team := &Team{
		ID:          dept.ID,
		Name:        dept.Name,
		DefaultLead: dept.DefaultLead,
		workerIndex: make(map[string]*Worker),
	}
	for _, roleID := range dept.Roles {
		role, ok := m.store.Role(roleID)
		if !ok {
			return fmt.Errorf("department %q lists unknown role %q", deptID, roleID)
		}
		subset := tools.NewRegistrySubset(m.base, role.Tools)
		ag, err := m.factory(subset, m.model)
		if err != nil {
			return fmt.Errorf("worker %q: %w", roleID, err)
		}
		sess := session.NewRoleSession(ag.ThreadID(), role.Name, ag, role.System)
		sess.MarkStarted(role.Name)
		w := &Worker{
			RoleID:      role.ID,
			TeamID:      team.ID,
			Name:        role.Name,
			Session:     sess,
			description: role.Description,
			roleSystem:  role.System,
			tools:       append([]string(nil), role.Tools...),
		}
		team.workers = append(team.workers, w)
		team.workerIndex[role.ID] = w
	}
	m.teams[deptID] = team
	m.order = append(m.order, deptID)
	return nil
}

// Team returns a team by id.
func (m *Manager) Team(id string) (*Team, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.teams[id]
	return t, ok
}

// Teams returns teams in YAML/creation order.
func (m *Manager) Teams() []*Team {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*Team, 0, len(m.order))
	for _, id := range m.order {
		out = append(out, m.teams[id])
	}
	return out
}

// ResolveRoleName maps a display name to a role id using longest case-insensitive match.
func (m *Manager) ResolveRoleName(display string) (roleID string, ok bool) {
	display = strings.TrimSpace(display)
	if display == "" {
		return "", false
	}
	lower := strings.ToLower(display)

	m.mu.Lock()
	defer m.mu.Unlock()

	var bestID string
	bestLen := -1
	seen := map[string]struct{}{}
	for _, team := range m.teams {
		for _, w := range team.workers {
			if _, dup := seen[w.RoleID]; dup {
				continue
			}
			seen[w.RoleID] = struct{}{}
			name := w.Name
			if strings.EqualFold(name, lower) || strings.HasPrefix(strings.ToLower(name), lower) {
				if len(name) > bestLen {
					bestLen = len(name)
					bestID = w.RoleID
				}
			}
		}
	}
	for _, team := range m.teams {
		for _, w := range team.workers {
			if strings.EqualFold(w.Name, display) && len(w.Name) > bestLen {
				bestLen = len(w.Name)
				bestID = w.RoleID
			}
		}
	}

	// Longest display name that matches as prefix of input or exact match across catalog
	allRoles := m.store.Roles()
	sort.Slice(allRoles, func(i, j int) bool {
		return len(allRoles[i].Name) > len(allRoles[j].Name)
	})
	for _, r := range allRoles {
		if strings.EqualFold(r.Name, display) {
			return r.ID, true
		}
	}
	for _, r := range allRoles {
		rl := strings.ToLower(r.Name)
		if strings.HasPrefix(lower, rl) {
			rest := strings.TrimSpace(display[len(r.Name):])
			if rest == "" || strings.HasPrefix(rest, " ") {
				if len(r.Name) > bestLen {
					return r.ID, true
				}
			}
		}
	}
	if bestID != "" {
		return bestID, true
	}
	return "", false
}

// ValidateTarget checks team and role membership.
func (m *Manager) ValidateTarget(teamID, roleID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	team, ok := m.teams[teamID]
	if !ok {
		return fmt.Errorf("unknown team %q", teamID)
	}
	if _, ok := team.workerIndex[roleID]; !ok {
		return fmt.Errorf("role %q is not on team %q", roleID, teamID)
	}
	return nil
}

// TryClaim marks a worker busy. Returns false if already claimed.
func (m *Manager) TryClaim(teamID, roleID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := claimKey{teamID: teamID, roleID: roleID}
	if _, busy := m.claims[key]; busy {
		return false
	}
	m.claims[key] = struct{}{}
	return true
}

// TryClaimOrPushNote tries to claim the worker under the manager lock, or enqueues
// note on the agent when the slot is already busy (same critical section).
func (m *Manager) TryClaimOrPushNote(teamID, roleID string, ag *agent.Agent, note string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := claimKey{teamID: teamID, roleID: roleID}
	if _, busy := m.claims[key]; busy {
		if ag != nil {
			ag.PushNote(note)
		}
		return false
	}
	m.claims[key] = struct{}{}
	return true
}

// FinishWorkerRun releases the worker claim unless the agent still has pending notes.
// The claim check and release are atomic with respect to TryClaimOrPushNote.
func (m *Manager) FinishWorkerRun(teamID, roleID string, ag *agent.Agent) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if ag != nil && ag.HasPendingNotes() {
		return false
	}
	delete(m.claims, claimKey{teamID: teamID, roleID: roleID})
	return true
}

// Release clears a claim.
func (m *Manager) Release(teamID, roleID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.claims, claimKey{teamID: teamID, roleID: roleID})
}

// IsClaimed reports whether a worker slot is busy.
func (m *Manager) IsClaimed(teamID, roleID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.claims[claimKey{teamID: teamID, roleID: roleID}]
	return ok
}

// AddDirective appends a standing directive and returns workers in that team that are currently claimed.
func (m *Manager) AddDirective(teamID, directive string) ([]*Worker, error) {
	directive = strings.TrimSpace(directive)
	if directive == "" {
		return nil, fmt.Errorf("directive is empty")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	team, ok := m.teams[teamID]
	if !ok {
		return nil, fmt.Errorf("unknown team %q", teamID)
	}
	team.directives = append(team.directives, directive)
	var running []*Worker
	for _, w := range team.workers {
		key := claimKey{teamID: teamID, roleID: w.RoleID}
		if _, busy := m.claims[key]; busy {
			running = append(running, w)
		}
	}
	return running, nil
}

// SetModel updates the model spec and all worker agents.
func (m *Manager) SetModel(model ModelSpec) {
	m.mu.Lock()
	m.model = model
	teams := make([]*Team, 0, len(m.order))
	for _, id := range m.order {
		teams = append(teams, m.teams[id])
	}
	m.mu.Unlock()

	for _, team := range teams {
		for _, w := range team.workers {
			w.Session.Agent.SetModel(model.Name, model.Provider, model.PromptPrice, model.CompletionPrice)
		}
	}
}

// ReloadRoles reloads YAML when every worker is idle.
func (m *Manager) ReloadRoles(ctx context.Context) error {
	m.mu.Lock()
	for key := range m.claims {
		m.mu.Unlock()
		return fmt.Errorf("worker %s/%s is busy", key.teamID, key.roleID)
	}
	if err := m.store.Reload(ctx); err != nil {
		m.mu.Unlock()
		return err
	}
	oldTeams := m.teams
	oldOrder := append([]string(nil), m.order...)
	m.mu.Unlock()

	for _, teamID := range oldOrder {
		dept, ok := m.store.Department(teamID)
		if !ok {
			continue
		}
		m.mu.Lock()
		team := oldTeams[teamID]
		if team == nil {
			m.mu.Unlock()
			continue
		}
		newIndex := make(map[string]*Worker)
		var newWorkers []*Worker
		for _, roleID := range dept.Roles {
			role, ok := m.store.Role(roleID)
			if !ok {
				m.mu.Unlock()
				return fmt.Errorf("department %q lists unknown role %q", teamID, roleID)
			}
			if existing, ok := team.workerIndex[roleID]; ok && roleUnchanged(existing, role) {
				newWorkers = append(newWorkers, existing)
				newIndex[roleID] = existing
				continue
			}
			subset := tools.NewRegistrySubset(m.base, role.Tools)
			ag, err := m.factory(subset, m.model)
			if err != nil {
				m.mu.Unlock()
				return err
			}
			sess := session.NewRoleSession(ag.ThreadID(), role.Name, ag, role.System)
			sess.MarkStarted(role.Name)
			w := &Worker{
				RoleID:      role.ID,
				TeamID:      team.ID,
				Name:        role.Name,
				Session:     sess,
				description: role.Description,
				roleSystem:  role.System,
				tools:       append([]string(nil), role.Tools...),
			}
			newWorkers = append(newWorkers, w)
			newIndex[roleID] = w
		}
		team.workers = newWorkers
		team.workerIndex = newIndex
		team.DefaultLead = dept.DefaultLead
		team.Name = dept.Name
		m.mu.Unlock()
	}
	for _, dept := range m.store.Departments() {
		if _, ok := oldTeams[dept.ID]; ok {
			continue
		}
		if err := m.CreateTeam(dept.ID); err != nil {
			return err
		}
	}
	return nil
}

func roleUnchanged(w *Worker, role roles.Role) bool {
	if w == nil || w.Name != role.Name {
		return false
	}
	if w.Session == nil || w.Session.Context == nil {
		return false
	}
	if len(w.Session.Context.Messages()) == 0 {
		return false
	}
	return w.description == role.Description && w.roleSystem == role.System && slices.Equal(w.tools, role.Tools)
}

// CatalogSnapshot builds JSON for routing.
func (m *Manager) CatalogSnapshot() CatalogSnapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	var snap CatalogSnapshot
	for _, id := range m.order {
		team := m.teams[id]
		if team == nil {
			continue
		}
		ct := CatalogTeam{
			ID:          team.ID,
			Name:        team.Name,
			DefaultLead: team.DefaultLead,
		}
		for _, w := range team.workers {
			_, busy := m.claims[claimKey{teamID: team.ID, roleID: w.RoleID}]
			ct.Roles = append(ct.Roles, CatalogRole{
				ID:          w.RoleID,
				Name:        w.Name,
				Description: w.description,
				Tools:       append([]string(nil), w.tools...),
				Busy:        busy,
			})
		}
		snap.Teams = append(snap.Teams, ct)
	}
	return snap
}

// WorkerFor returns a worker on a team.
func (m *Manager) WorkerFor(teamID, roleID string) (*Worker, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	team, ok := m.teams[teamID]
	if !ok {
		return nil, false
	}
	w, ok := team.workerIndex[roleID]
	return w, ok
}

// TeamForRole finds the team that hosts a role id (first match in order).
func (m *Manager) TeamForRole(roleID string) (teamID string, ok bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, id := range m.order {
		team := m.teams[id]
		if _, ok := team.workerIndex[roleID]; ok {
			return id, true
		}
	}
	return "", false
}

// FormatTeamsList renders /teams output.
func (m *Manager) FormatTeamsList() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	var b strings.Builder
	for _, id := range m.order {
		team := m.teams[id]
		fmt.Fprintf(&b, "Team %s (%s), lead=%s\n", team.Name, team.ID, team.DefaultLead)
		for _, w := range team.workers {
			status := "idle"
			if _, busy := m.claims[claimKey{teamID: team.ID, roleID: w.RoleID}]; busy {
				status = "busy"
			}
			fmt.Fprintf(&b, "  - %s [%s] (%s)\n", w.Name, w.RoleID, status)
		}
		if len(team.directives) > 0 {
			b.WriteString("  Directives:\n")
			for _, d := range team.directives {
				fmt.Fprintf(&b, "    • %s\n", d)
			}
		}
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// TaskUserContent formats standing directives and task text under the manager lock.
func (m *Manager) TaskUserContent(teamID, task string) string {
	m.mu.Lock()
	team := m.teams[teamID]
	var directives []string
	if team != nil {
		directives = append([]string(nil), team.directives...)
	}
	m.mu.Unlock()

	var sb strings.Builder
	if len(directives) > 0 {
		sb.WriteString("[Standing directives]\n")
		for _, d := range directives {
			sb.WriteString("- ")
			sb.WriteString(d)
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
	}
	sb.WriteString(task)
	return sb.String()
}
