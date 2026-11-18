package uuwindow

import (
	"slices"
	"strings"
)

const (
	BundleID = "com.netease.uuremote"
	Process  = "UURemote"

	pageTitle      = "本机被控中"
	controlMark    = "远程控制"
	durationPrefix = "连接时长"
)

type Kind int

const (
	Other Kind = iota
	Text
	Table
	Row
)

type Element struct {
	Kind     Kind
	Text     string
	Children []Element
}

func Controllers(windows []Element) []string {
	var names []string
	for _, w := range windows {
		for _, table := range localTables(w) {
			for _, row := range rows(table) {
				texts := texts(row)
				if !slices.Contains(texts, controlMark) {
					continue
				}
				for _, t := range texts {
					t = strings.TrimSpace(t)
					if t == "" || t == controlMark || strings.HasPrefix(t, durationPrefix) {
						continue
					}
					if !slices.Contains(names, t) {
						names = append(names, t)
					}
					break
				}
			}
		}
	}
	return names
}

func localTables(e Element) []Element {
	var out []Element
	titled := false
	for _, c := range e.Children {
		titled = titled || (c.Kind == Text && c.Text == pageTitle)
	}
	if titled {
		for _, c := range e.Children {
			if c.Kind == Table {
				out = append(out, c)
			}
		}
	}
	for _, c := range e.Children {
		out = append(out, localTables(c)...)
	}
	return out
}

func rows(e Element) []Element {
	var out []Element
	if e.Kind == Row {
		out = append(out, e)
	}
	for _, c := range e.Children {
		out = append(out, rows(c)...)
	}
	return out
}

func texts(e Element) []string {
	var out []string
	if e.Kind == Text {
		out = append(out, e.Text)
	}
	for _, c := range e.Children {
		out = append(out, texts(c)...)
	}
	return out
}
