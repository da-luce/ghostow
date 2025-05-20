package tree

import "fmt"

type TreeNode struct {
	Text     string
	Icon     string                        // Optional prefix icon (like ✔, ✖)
	Color    func(a ...interface{}) string // Optional color printer
	Children []*TreeNode                   // Child nodes
}

func PrintTreeNode(node *TreeNode, prefix string, isLast bool) {
	// Choose the connector: ├─ for mid items, ╰─ for last
	connector := "├─ "
	if isLast {
		connector = "╰─ "
	}

	// Format the line
	line := prefix + connector
	if node.Icon != "" {
		line += node.Icon + " "
	}
	line += node.Text

	// Apply color if present
	if node.Color != nil {
		line = node.Color(line)
	}

	fmt.Println(line)

	// Prepare new prefix for children
	newPrefix := prefix
	if isLast {
		newPrefix += "   "
	} else {
		newPrefix += "│  "
	}

	// Recursively print children
	for i, child := range node.Children {
		PrintTreeNode(child, newPrefix, i == len(node.Children)-1)
	}
}
