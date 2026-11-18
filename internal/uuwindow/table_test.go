package uuwindow

import (
	"slices"
	"testing"
)

func text(s string) Element { return Element{Kind: Text, Text: s} }

func node(k Kind, children ...Element) Element { return Element{Kind: k, Children: children} }

func controllerRow(name string) Element {
	return node(Row, node(Other, text(name)), text("远程控制"), text("连接时长 00:10"))
}

func TestControllersOnTheLocalPage(t *testing.T) {
	win := node(Other,
		node(Other, text("本机被控中"), text("1")),
		node(Other,
			text("本机被控中"),
			node(Table, controllerRow("Rocky的iPhone"), controllerRow("Rocky的iPhone")),
		),
	)
	if got := Controllers([]Element{win}); !slices.Equal(got, []string{"Rocky的iPhone"}) {
		t.Errorf("controllers %q", got)
	}
}

func TestControllersIgnoreOtherPages(t *testing.T) {
	detail := node(Other,
		text("MacBook Air"),
		node(Table, controllerRow("Someone's iPad")),
	)
	home := node(Other, text("本机被控中"), node(Other, text("远程控制")))
	assist := node(Other, text("本机被控中"), node(Table, node(Row, text("Rocky的iPad"), text("远程协助"))))
	if got := Controllers([]Element{node(Other, detail, home, assist)}); len(got) != 0 {
		t.Errorf("controllers from other pages: %q", got)
	}
}

func TestControllersSkipBlankNames(t *testing.T) {
	win := node(Other, text("本机被控中"), node(Table, node(Row, text(" "), text("远程控制"), text("Rocky的iPad"))))
	if got := Controllers([]Element{win}); !slices.Equal(got, []string{"Rocky的iPad"}) {
		t.Errorf("controllers %q", got)
	}
}
