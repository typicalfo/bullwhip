package bw

import (
	"fmt"
	"io"
	"strings"
	"time"
)

func reviewLine(r Review) string {
	holder := r.Reviewer
	if holder == "" {
		holder = "-"
	}
	label := ""
	if r.Effort != "" {
		label = " effort=" + r.Effort
	}
	if len(r.Roles) > 0 {
		label += " roles=" + strings.Join(r.Roles, ",")
	}
	if r.Self {
		label = " SELF-REVIEW"
	}
	return fmt.Sprintf("%s  [%s]%s\n  reviewer: %s\n  head: %s", clean(r.ID), clean(r.State), label, clean(holder), clean(r.Head))
}
func eventLine(e Event) string {
	parts := []string{e.TS.Format(time.RFC3339), clean(e.Actor), clean(e.Event)}
	if e.ID > 0 {
		parts = append(parts, fmt.Sprintf("#%d", e.ID))
	}
	if e.Task != "" {
		parts = append(parts, "("+clean(e.Task)+")")
	}
	if e.Technical {
		parts = append(parts, "[technical]")
	}
	if e.Head != "" {
		parts = append(parts, "head="+clean(e.Head))
	}
	if e.Model != "" {
		parts = append(parts, "model="+clean(e.Model))
	}
	line := strings.Join(parts, " ")
	if e.Note != "" {
		line += "\n  " + strings.ReplaceAll(displayText(e.Note), "\n", "\n  ")
	}
	return line
}
func printBoard(w io.Writer, value any) bool {
	switch v := value.(type) {
	case Review:
		fmt.Fprintln(w, reviewLine(v))
		fmt.Fprintf(w, "  task: %s\n  branch: %s\n  author: %s\n", clean(v.Task), clean(v.Branch), clean(v.Author))
		for _, e := range v.Events {
			fmt.Fprintln(w, eventLine(e))
		}
		if v.Findings != "" {
			fmt.Fprintln(w, "Findings:\n"+v.Findings)
		}
		if v.Unchanged {
			fmt.Fprintln(w, "(already so; nothing changed)")
		}
	case []Review:
		if len(v) == 0 {
			fmt.Fprintln(w, "No reviews.")
		}
		for i, r := range v {
			if i > 0 {
				fmt.Fprintln(w)
			}
			fmt.Fprintln(w, reviewLine(r))
		}
	case Event:
		fmt.Fprintln(w, eventLine(v))
	case []Event:
		if len(v) == 0 {
			fmt.Fprintln(w, "No notes.")
		}
		for i, e := range v {
			if i > 0 {
				fmt.Fprintln(w)
			}
			fmt.Fprintln(w, eventLine(e))
		}
	case Question:
		fmt.Fprintln(w, eventLine(v.Event))
		if v.Unchanged {
			fmt.Fprintln(w, "(already so; nothing changed)")
		}
	case []Question:
		if len(v) == 0 {
			fmt.Fprintln(w, "No open questions; --all shows history.")
		}
		for i, q := range v {
			if i > 0 {
				fmt.Fprintln(w)
			}
			fmt.Fprintln(w, eventLine(q.Event))
			for _, e := range q.Answers {
				fmt.Fprintln(w, "  "+eventLine(e))
			}
		}
	default:
		return false
	}
	return true
}
