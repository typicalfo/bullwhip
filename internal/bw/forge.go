package bw

import (
	"net/url"
	"path"
	"strings"
)

type forgeLinks struct{ base, branch string }

func forgeFor(c Catalog, root string) forgeLinks {
	base := c.RepoURL
	if base == "" {
		b, _ := git(root, nil, "remote", "get-url", "origin")
		remote := strings.TrimSpace(string(b))
		if !strings.Contains(remote, "://") {
			if host, p, ok := strings.Cut(remote, ":"); ok {
				_, h, has := strings.Cut(host, "@")
				if has {
					host = h
				}
				remote = "https://" + host + "/" + p
			}
		}
		u, e := url.Parse(remote)
		if e == nil && u.Host != "" {
			base = "https://" + u.Host + strings.TrimSuffix(u.Path, ".git")
		}
	}
	branch := "HEAD"
	b, e := git(root, nil, "symbolic-ref", "--short", "refs/remotes/origin/HEAD")
	if e == nil {
		branch = strings.TrimPrefix(strings.TrimSpace(string(b)), "origin/")
	}
	return forgeLinks{strings.TrimRight(base, "/"), branch}
}
func (f forgeLinks) commit(sha string) string {
	return f.base + "/commit/" + url.PathEscape(sha)
}
func (f forgeLinks) task(v View) View {
	if f.base == "" {
		return v
	}
	prefix := "/blob/"
	v.Links = map[string]string{"catalog": f.base + prefix + url.PathEscape(f.branch) + "/" + catalogPath}
	if v.Ref == "" {
		return v
	}
	p, a, _ := strings.Cut(v.Ref, "#")
	u, e := url.Parse(p)
	if e != nil || u.IsAbs() || u.Host != "" || strings.HasPrefix(p, "/") || path.Clean(p) == ".." || strings.HasPrefix(path.Clean(p), "../") {
		return v
	}

	link := f.base + prefix + url.PathEscape(f.branch) + "/" + strings.Join(escapeParts(strings.Split(p, "/")), "/")
	if a != "" {
		link += "#" + url.PathEscape(a)
	}
	v.Links["ref"] = link
	return v
}
func escapeParts(parts []string) []string {
	for i, p := range parts {
		parts[i] = url.PathEscape(p)
	}
	return parts
}
func (f forgeLinks) review(r Review, c Catalog) Review {
	r = reviewMetadata(c, r)
	if f.base == "" {
		return r
	}
	r.Links = map[string]string{}
	if r.Head != "" {
		r.Links["head"] = f.commit(r.Head)
	}
	r.Links["branch"] = f.base + "/tree/" + url.PathEscape(r.Branch)
	r.Links["search"] = f.base + "/pulls?q=" + url.QueryEscape("is:pr head:"+r.Branch)
	if r.MergedSHA != "" {
		r.Links["merged"] = f.commit(r.MergedSHA)
	}
	for _, e := range r.Events {
		if e.Event == "merged" {
			r.Links["merged"] = f.commit(e.Head)
		}
	}
	return r
}
func addLinks(v any, c Catalog, root string) any {
	f := forgeFor(c, root)
	switch x := v.(type) {
	case Todo:
		x.Tasks = addLinks(x.Tasks, c, root).(TaskList)
		return x
	case WorkerList:
		for i := range x {
			for j := range x[i].Current {
				x[i].Current[j] = f.task(x[i].Current[j])
			}
			for j := range x[i].Tasks {
				for k := range x[i].Tasks[j].Rounds {
					r := &x[i].Tasks[j].Rounds[k]
					r.Links = f.review(Review{ID: r.Review, Branch: r.Branch, Head: r.Head}, c).Links
				}
				for _, t := range c.Tasks {
					if t.ID == x[i].Tasks[j].ID {
						x[i].Tasks[j].Links = f.task(View{Task: t}).Links
					}
				}
			}
			for j := range x[i].Reviews {
				x[i].Reviews[j] = f.review(x[i].Reviews[j], c)
			}
		}
		return x
	case View:
		return f.task(x)
	case []View:
		for i := range x {
			x[i] = f.task(x[i])
		}
		return x
	case TaskList:
		for i := range x {
			x[i].View = f.task(x[i].View)
		}
		return x
	case Review:
		return f.review(x, c)
	case []Review:
		for i := range x {
			x[i] = f.review(x[i], c)
		}
		return x
	case Statistics:
		for i := range x.Tasks {
			for k := range x.Tasks[i].Rounds {
				r := &x.Tasks[i].Rounds[k]
				r.Links = f.review(Review{ID: r.Review, Branch: r.Branch, Head: r.Head}, c).Links
			}
			for _, t := range c.Tasks {
				if t.ID == x.Tasks[i].ID {
					x.Tasks[i].Links = f.task(View{Task: t}).Links
					break
				}
			}
		}
		for i := range x.Workers {
			for j := range x.Workers[i].Current {
				x.Workers[i].Current[j] = f.task(x.Workers[i].Current[j])
			}
		}
		return x
	case Holdings:
		for i := range x.Claims {
			for _, t := range c.Tasks {
				if t.ID == x.Claims[i].ID {
					x.Claims[i].Links = f.task(View{Task: t}).Links
					break
				}
			}
		}
		for i := range x.Reviews {
			r := x.Reviews[i]
			x.Reviews[i].Links = f.review(Review{ID: r.ID, Task: r.Task, Branch: r.Branch}, c).Links
		}
		return x
	case Summary:
		for i := range x.Claims {
			x.Claims[i] = f.task(x.Claims[i])
		}
		for i := range x.Reviews {
			x.Reviews[i] = f.review(x.Reviews[i], c)
		}
		return x
	}
	return v
}

func reviewMetadata(c Catalog, r Review) Review {
	r.Effort = reviewEffort(c, r.Task)
	for _, t := range c.Tasks {
		if t.ID == r.Task {
			r.Roles = t.ReviewRoles
			break
		}
	}
	return r
}
