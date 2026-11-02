package control

// Status is what `status` says about the open window.
type Status struct {
	Window       bool                  `json:"window"`
	Socket       string                `json:"socket"`
	Space        *Space                `json:"space,omitempty"`
	Tab          int                   `json:"tab,omitempty"`
	Pane         *Pane                 `json:"pane,omitempty"`
	Spaces       int                   `json:"spaces"`
	Tabs         int                   `json:"tabs"`
	Sessions     int                   `json:"sessions"`
	Agents       int                   `json:"agents"`
	Attention    int                   `json:"attention"`
	IndexOpen    bool                  `json:"indexOpen"`
	Core         bool                  `json:"core"`
	CoreInstance string                `json:"coreInstance,omitempty"`
	Stacks       map[string]string     `json:"stacks,omitempty"` // layout stack -> shown child
	Floats       map[string][2]float32 `json:"floats,omitempty"` // float id -> drag offset
	Sizes        map[string]float32    `json:"sizes,omitempty"`  // resized layout block -> pixels
}

// Space is one space (a workspace in the code) and its tabs.
type Space struct {
	ID      int    `json:"id"`
	Name    string `json:"name"`
	Folder  string `json:"folder"`
	Machine string `json:"machine,omitempty"`
	Active  bool   `json:"active"`
	Tabs    []Tab  `json:"tabs"`
}

// Tab is one tab of a space.
type Tab struct {
	Index  int    `json:"index"` // 1 = the first
	Active bool   `json:"active"`
	Panes  []Pane `json:"panes"`
}

// Pane is one session on screen.
type Pane struct {
	ID             int    `json:"id"`
	Kind           string `json:"kind"` // claude, codex, opencode or shell
	Title          string `json:"title"`
	Detail         string `json:"detail,omitempty"`
	Cwd            string `json:"cwd"`
	Machine        string `json:"machine,omitempty"`
	Focused        bool   `json:"focused"`
	Agent          bool   `json:"agent"`
	App            string `json:"app,omitempty"`      // the program's own 7501 name
	Word           string `json:"word,omitempty"`     // the program's own 7501 state
	Message        string `json:"message,omitempty"`  // the program's 7501 message
	Progress       int    `json:"progress,omitempty"` // 0-100, when it reported one
	State          string `json:"state"`              // idle, working, blocked, done, running or stopped
	Cols           int    `json:"cols"`
	Rows           int    `json:"rows"`
	ScrollMode     bool   `json:"scrollMode,omitempty"`
	Scrolled       int    `json:"scrolled,omitempty"`
	Visual         bool   `json:"visual,omitempty"`
	VisualLinewise bool   `json:"visualLinewise,omitempty"`
	VisualFrom     int    `json:"visualFrom,omitempty"`
	VisualTo       int    `json:"visualTo,omitempty"`
	VisualFromCol  int    `json:"visualFromCol,omitempty"`
	VisualToCol    int    `json:"visualToCol,omitempty"`
}

// List is what `ls` says: every space, tab and session in the window.
type List struct {
	Spaces []Space `json:"spaces"`
}
