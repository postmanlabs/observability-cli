package apidiff

import (
	"encoding/json"
	"io"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/pkg/errors"
	"github.com/rivo/tview"
	jd "github.com/yudai/gojsondiff"
	"github.com/yudai/gojsondiff/formatter"

	"github.com/akitasoftware/akita-libs/path_trie"
)

const (
	rootPageID = "xxx-akita-root-page"
)

// Combined parts of spec_diff.SpecAndDiffKind and
// spec_diff.ChangedSpecsAndDiffKind that is needed for presentation.
type leafVal struct {
	// "added", "removed", or "changed
	DiffKind string `json:"diff_kind"`

	// Present if diff_kind is "added" or "removed"
	Spec *string `json:"spec,omitempty"`

	// Present if diff_kind is "changed"
	BaseSpec *string `json:"base_spec,omitempty"`
	NewSpec  *string `json:"new_spec,omitempty"`
}

func interactiveDisplay(diff *path_trie.PathTrie) error {
	root, hasRoot := diff.Trie[""]
	if !hasRoot {
		return errors.Errorf("trie does not have a root")
	}

	app := tview.NewApplication()
	diffPages := tview.NewPages()

	// Create a hidden page for each leaf node in the trie. Each page contains a
	// diff.
	if err := createDiffPages(diffPages, diff.PathSeparator, root); err != nil {
		return errors.Wrap(err, "failed to create diff pages")
	}

	// Create the actual tree structure.
	rootNode, err := convertNode(diffPages, diff.PathSeparator, root)
	if err != nil {
		return errors.Wrap(err, "failed to create root node")
	}
	tree := tview.NewTreeView().
		SetRoot(rootNode).
		SetCurrentNode(rootNode)

	// Create the root page that contains the tree and is the page that each diff
	// page comes back to.
	rootPage := diffPages.AddPage(rootPageID, tree, true, true)
	rootPage.SetBackgroundColor(tcell.ColorDefault)

	// Allow user to use 'q' to quit.
	app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if r := event.Rune(); r == 'q' || r == 'Q' {
			app.Stop()
		}
		return event
	})

	app.SetBeforeDrawFunc(func(screen tcell.Screen) bool {
		// Clear the screen on every draw so each diff page maintains a clear
		// background.
		screen.Clear()
		return false
	})

	rootFrame := tview.NewFrame(rootPage)
	// To go from individual diff pages back to the root page, users need to use
	// ESC. However, if we add the ESC help text as a separate frame on each diff
	// page, we get nested frames with both instructions. Plus, the 'q' escape
	// behavior is applied to all pages. Hence, we put both help text ('q' and
	// ESC) in the root frame.
	rootFrame.AddText(`q to quit, ESC to go back`, false, 0, tcell.ColorYellow)
	rootFrame.SetBackgroundColor(tcell.ColorDefault)

	return app.SetRoot(rootFrame, true).SetFocus(rootFrame).Run()
}

func getKey(sep string, trieNode *path_trie.PathTrieNode) string {
	return trieNode.Prefix + sep + trieNode.Name
}

func getString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func createDiffPages(diffPages *tview.Pages, sep string, trieNode *path_trie.PathTrieNode) error {
	if len(trieNode.Children) == 0 {
		leaf, err := getLeafVal(trieNode)
		if err != nil {
			return errors.Wrapf(err, "failed to get leaf node value %s", trieNode.Name)
		}

		var baseSpec, newSpec string
		switch leaf.DiffKind {
		case "added":
			baseSpec = "{}"
			newSpec = getString(leaf.Spec)
		case "removed":
			baseSpec = getString(leaf.Spec)
			newSpec = "{}"
		case "changed":
			baseSpec = getString(leaf.BaseSpec)
			newSpec = getString(leaf.NewSpec)
		}

		// This is a leaf, create a page with the diff.
		txt := tview.NewTextView()
		txt.SetDynamicColors(true)
		txt.SetScrollable(true)
		txt.SetDoneFunc(func(k tcell.Key) {
			// Go back to root page when user presses ESC. The help text is included
			// in the rootFrame above.
			if k == tcell.KeyEsc {
				diffPages.SwitchToPage(rootPageID)
			}
		})
		txt.SetBackgroundColor(tcell.ColorDefault)

		if err := writeDiff(tview.ANSIWriter(txt), baseSpec, newSpec); err != nil {
			return errors.Wrapf(err, "failed to create diff text for %s", trieNode.Name)
		}

		diffPages.AddPage(getKey(sep, trieNode), txt, true, false)
	} else {
		for _, child := range trieNode.Children {
			if err := createDiffPages(diffPages, sep, child); err != nil {
				return errors.Wrapf(err, "failed to create child node %s", child.Name)
			}
		}
	}
	return nil
}

func convertNode(diffPages *tview.Pages, sep string, trieNode *path_trie.PathTrieNode) (*tview.TreeNode, error) {
	key := getKey(sep, trieNode)
	node := tview.NewTreeNode(trieNode.Name)
	node.SetReference(key)

	if len(trieNode.Children) == 0 {
		// This is a leaf. Create a link to the corresponding diff page.
		node.SetSelectable(true)
		node.SetSelectedFunc(func() {
			diffPages.SwitchToPage(key)
		})

		leaf, err := getLeafVal(trieNode)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to create leaf node %s", trieNode.Name)
		}

		switch leaf.DiffKind {
		case "added":
			node.SetColor(tcell.ColorGreen)
		case "removed":
			node.SetColor(tcell.ColorRed)
		case "changed":
			node.SetColor(tcell.ColorYellow)
		}
	} else {
		for _, child := range trieNode.Children {
			if childNode, err := convertNode(diffPages, sep, child); err == nil {
				node.AddChild(childNode)
			} else {
				return nil, errors.Wrapf(err, "failed to create child node %s", child.Name)
			}
		}
	}

	return node, nil
}

func getLeafVal(trieNode *path_trie.PathTrieNode) (*leafVal, error) {
	// TODO(kku): preserve the trie value as json.RawMessage so we don't need to
	// encode and decode here.
	j, err := json.Marshal(trieNode.Val)
	if err != nil {
		return nil, errors.Wrap(err, "node value is not valid json")
	}

	var leaf leafVal
	if err := json.Unmarshal(j, &leaf); err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal JSON into leafVal")
	}
	return &leaf, nil
}

func writeDiff(w io.Writer, baseSpec, newSpec string) error {
	differ := jd.New()
	diff, err := differ.Compare([]byte(baseSpec), []byte(newSpec))
	if err != nil {
		return errors.Wrap(err, "failed to generate diff")
	}

	var baseJSON interface{}
	if err := json.Unmarshal([]byte(baseSpec), &baseJSON); err != nil {
		return errors.Wrap(err, "failed to parse base spec as JSON")
	}

	config := formatter.AsciiFormatterConfig{
		ShowArrayIndex: true,
		Coloring:       true,
	}
	f := formatter.NewAsciiFormatter(baseJSON, config)
	diffString, err := f.Format(diff)
	if err != nil {
		return errors.Wrap(err, "failed to generate diff string")
	}

	_, err = io.Copy(w, strings.NewReader(diffString))
	return err
}
