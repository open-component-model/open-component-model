package explorer

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/tui/fetch"
)

// Messages

type versionsMsg struct {
	component string
	versions  []string
}

type descriptorMsg struct {
	component  string
	version    string
	descriptor *descriptor.Descriptor
}

type errMsg struct{ err error }

func (e errMsg) Error() string { return e.err.Error() }

// Model is the bubbletea model for the component explorer view.
type Model struct {
	fetcher fetch.ComponentFetcher
	keys    KeyMap

	// Tree state
	roots   []*Node
	cursor  int
	visible []*Node

	// Detail pane
	detail viewport.Model

	// Layout
	width      int
	height     int
	treeWidth  int
	focusTree  bool
	ready      bool

	// Status
	err     error
	loading bool

	// Initial component to load
	initialComponent string
	initialVersion   string
}

// New creates a new explorer model.
func New(fetcher fetch.ComponentFetcher, component, version string) Model {
	return Model{
		fetcher:          fetcher,
		keys:             DefaultKeyMap(),
		focusTree:        true,
		initialComponent: component,
		initialVersion:   version,
	}
}

func (m Model) Init() tea.Cmd {
	if m.initialComponent == "" {
		return nil
	}

	// If we have a specific version, fetch its descriptor directly.
	if m.initialVersion != "" {
		return m.fetchDescriptor(m.initialComponent, m.initialVersion)
	}
	// Otherwise list versions for the component.
	return m.fetchVersions(m.initialComponent)
}

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.treeWidth = msg.Width / 2
		detailWidth := msg.Width - m.treeWidth - 1
		contentHeight := msg.Height - 3 // status bar + help bar
		if !m.ready {
			m.detail = viewport.New(detailWidth, contentHeight)
			m.ready = true
		} else {
			m.detail.Width = detailWidth
			m.detail.Height = contentHeight
		}
		return m, nil

	case versionsMsg:
		m.loading = false
		node := &Node{
			Kind:    NodeComponent,
			Label:   msg.component,
			Depth:   0,
			Loading: false,
		}
		for _, v := range msg.versions {
			node.Children = append(node.Children, &Node{
				Kind:  NodeVersion,
				Label: v,
				Depth: 1,
			})
		}
		node.Expanded = true
		m.roots = []*Node{node}
		m.visible = Flatten(m.roots)
		m.updateDetail()
		return m, nil

	case descriptorMsg:
		m.loading = false
		found := false
		// Find the version node and populate it.
		for _, root := range m.roots {
			for _, child := range root.Children {
				if child.Kind == NodeVersion && child.Label == msg.version {
					child.Descriptor = msg.descriptor
					child.Children = BuildVersionNodes(msg.descriptor, child.Depth+1)[0].Children
					child.Expanded = true
					child.Loading = false
					found = true
					break
				}
			}
		}
		// If no existing tree, create one from the descriptor.
		if !found && msg.descriptor != nil {
			node := &Node{
				Kind:     NodeComponent,
				Label:    msg.descriptor.Component.Name,
				Depth:    0,
				Expanded: true,
			}
			versionNodes := BuildVersionNodes(msg.descriptor, 1)
			versionNodes[0].Expanded = true
			node.Children = versionNodes
			m.roots = []*Node{node}
		}
		m.visible = Flatten(m.roots)
		m.updateDetail()
		return m, nil

	case errMsg:
		m.loading = false
		m.err = msg.err
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	// Forward to detail viewport if focused.
	if !m.focusTree {
		var cmd tea.Cmd
		m.detail, cmd = m.detail.Update(msg)
		return m, cmd
	}

	return m, nil
}

func (m Model) handleKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Up):
		if m.focusTree && m.cursor > 0 {
			m.cursor--
			m.updateDetail()
		}

	case key.Matches(msg, m.keys.Down):
		if m.focusTree && m.cursor < len(m.visible)-1 {
			m.cursor++
			m.updateDetail()
		}

	case key.Matches(msg, m.keys.PageUp):
		if m.focusTree {
			m.cursor -= 10
			if m.cursor < 0 {
				m.cursor = 0
			}
			m.updateDetail()
		}

	case key.Matches(msg, m.keys.PageDown):
		if m.focusTree {
			m.cursor += 10
			if m.cursor >= len(m.visible) {
				m.cursor = len(m.visible) - 1
			}
			if m.cursor < 0 {
				m.cursor = 0
			}
			m.updateDetail()
		}

	case key.Matches(msg, m.keys.Expand):
		if m.focusTree && len(m.visible) > 0 {
			node := m.visible[m.cursor]
			if node.IsExpandable() && !node.Expanded {
				node.Expanded = true
				// If a version node has no children yet, fetch the descriptor.
				if node.Kind == NodeVersion && len(node.Children) == 0 && node.Descriptor == nil {
					node.Loading = true
					m.visible = Flatten(m.roots)
					// Find parent component name.
					component := m.findComponentName(node)
					return m, m.fetchDescriptor(component, node.Label)
				}
				// If a reference node, fetch versions for the referenced component.
				if node.Kind == NodeReference && len(node.Children) == 0 && node.Reference != nil {
					node.Loading = true
					m.visible = Flatten(m.roots)
					return m, m.fetchDescriptor(node.Reference.Component, node.Reference.Version)
				}
				m.visible = Flatten(m.roots)
				m.updateDetail()
			}
		}

	case key.Matches(msg, m.keys.Collapse):
		if m.focusTree && len(m.visible) > 0 {
			node := m.visible[m.cursor]
			if node.IsExpandable() && node.Expanded {
				node.Expanded = false
				m.visible = Flatten(m.roots)
				m.updateDetail()
			}
		}
	}

	return m, nil
}

func (m *Model) updateDetail() {
	if len(m.visible) == 0 || m.cursor >= len(m.visible) {
		m.detail.SetContent("No items.")
		return
	}
	node := m.visible[m.cursor]
	m.detail.SetContent(NodeDetail(node))
	m.detail.GotoTop()
}

func (m Model) findComponentName(node *Node) string {
	for _, root := range m.roots {
		if root.Kind == NodeComponent {
			for _, child := range root.Children {
				if child == node {
					return root.Label
				}
			}
			// Check deeper - the node might be nested.
			if containsNode(root, node) {
				return root.Label
			}
		}
	}
	return m.initialComponent
}

func containsNode(parent, target *Node) bool {
	for _, child := range parent.Children {
		if child == target {
			return true
		}
		if containsNode(child, target) {
			return true
		}
	}
	return false
}

func (m Model) fetchVersions(component string) tea.Cmd {
	return func() tea.Msg {
		versions, err := m.fetcher.ListVersions(context.Background(), component)
		if err != nil {
			return errMsg{fmt.Errorf("listing versions for %s: %w", component, err)}
		}
		return versionsMsg{component: component, versions: versions}
	}
}

func (m Model) fetchDescriptor(component, version string) tea.Cmd {
	return func() tea.Msg {
		desc, err := m.fetcher.GetDescriptor(context.Background(), component, version)
		if err != nil {
			return errMsg{fmt.Errorf("fetching %s:%s: %w", component, version, err)}
		}
		return descriptorMsg{component: component, version: version, descriptor: desc}
	}
}

func (m Model) View() string {
	if !m.ready {
		return "Initializing..."
	}

	contentHeight := m.height - 3

	// Tree pane
	treeContent := m.renderTree(contentHeight)
	treeView := lipgloss.NewStyle().
		Width(m.treeWidth).
		Height(contentHeight).
		Render(treeContent)

	// Detail pane
	detailView := lipgloss.NewStyle().
		Width(m.width - m.treeWidth - 1).
		Height(contentHeight).
		PaddingLeft(1).
		Render(m.detail.View())

	// Join panes horizontally.
	content := lipgloss.JoinHorizontal(lipgloss.Top, treeView, "│", detailView)

	// Help bar
	helpLine := m.renderHelp()

	return lipgloss.JoinVertical(lipgloss.Left, content, helpLine)
}

func (m Model) renderTree(height int) string {
	if m.loading && len(m.visible) == 0 {
		return "Loading..."
	}
	if m.err != nil && len(m.visible) == 0 {
		return fmt.Sprintf("Error: %v", m.err)
	}

	var lines []string

	// Calculate scroll offset to keep cursor visible.
	scrollOffset := 0
	if m.cursor >= height {
		scrollOffset = m.cursor - height + 1
	}

	end := scrollOffset + height
	if end > len(m.visible) {
		end = len(m.visible)
	}

	for i := scrollOffset; i < end; i++ {
		node := m.visible[i]
		line := RenderNode(node, i == m.cursor)

		if i == m.cursor {
			if m.focusTree {
				line = lipgloss.NewStyle().Bold(true).Reverse(true).Render(line)
			} else {
				line = lipgloss.NewStyle().Bold(true).Render(line)
			}
		}

		lines = append(lines, line)
	}

	return strings.Join(lines, "\n")
}

func (m Model) renderHelp() string {
	var parts []string
	parts = append(parts, "j/k: navigate")
	parts = append(parts, "enter: expand")
	parts = append(parts, "esc: collapse")
	parts = append(parts, "tab: switch pane")
	parts = append(parts, "q: quit")

	help := strings.Join(parts, "  ")
	return lipgloss.NewStyle().
		Foreground(lipgloss.AdaptiveColor{Light: "#999999", Dark: "#666666"}).
		Render(help)
}

// FocusTree returns whether the tree pane is focused.
func (m Model) FocusTree() bool {
	return m.focusTree
}

// ToggleFocus switches focus between tree and detail panes.
func (m *Model) ToggleFocus() {
	m.focusTree = !m.focusTree
}

// Err returns any error that occurred during data fetching.
func (m Model) Err() error {
	return m.err
}
