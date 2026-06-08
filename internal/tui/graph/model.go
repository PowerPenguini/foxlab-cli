package graph

type Model struct {
	ID    string
	Nodes []Node
	Edges []Edge
}

type Node struct {
	ID      string
	Type    string
	Badge   string
	Label   string
	State   string
	X       int
	Y       int
	Details []string
}

type Edge struct {
	From string
	To   string
}

func Key(typ, id string) string {
	return typ + ":" + id
}

func (n Node) Key() string {
	return Key(n.Type, n.ID)
}
