package commits

import "slices"

// maxLanes is how many lanes the web UI draws; a lane beyond them draws in the last.
const maxLanes = 8

// Edge is a line through a row of the git graph, from a lane at its top (or its node) to a lane at its bottom (or its node).
type Edge struct {
	From, To int
	Color    int  // the lane's colour, 0..5
	Open     bool // the line leads to a parent older than the stored history
}

// Row is how one commit's row of the git graph draws: its node, the lines in from above and the lines out below.
type Row struct {
	Lane, Color  int
	Merge        bool
	Above, Below []Edge
}

// layout lays out commits in graph order like `git log --graph`: a commit takes the lane that expects it (else the first free one),
// every other lane expecting it converges into it, its first parent continues the lane, and each further parent opens a lane or
// joins the one already expecting it. Freed lanes are reused and lanes never shift, so a row depends only on the rows above it.
// held reports whether a parent is in the history; a line to one that is not is open.
func layout(order []*Commit, held func(sha string) bool) []Row {
	var lanes []string // the parent each lane expects; "" is free
	var colors []int
	opened := 0
	set := func(lane int, sha string) {
		for len(lanes) <= lane {
			lanes = append(lanes, "")
		}
		lanes[lane] = sha
	}
	open := func(lane int) {
		for len(colors) <= lane {
			colors = append(colors, 0)
		}
		colors[lane] = 0
		if lane != 0 || opened != 0 {
			colors[lane] = 1 + opened%5
		}
		opened++
	}
	free := func() int {
		if i := slices.Index(lanes, ""); i >= 0 {
			return i
		}
		return len(lanes)
	}
	rows := make([]Row, len(order))
	for i, c := range order {
		before := slices.Clone(lanes)
		lane := slices.Index(before, c.SHA)
		if lane < 0 {
			lane = free()
			open(lane)
		}
		r := Row{Lane: lane, Merge: len(c.Parents) > 1, Above: []Edge{}, Below: []Edge{}}
		for j, sha := range before {
			if sha == "" {
				continue
			}
			to := j
			if sha == c.SHA {
				to, lanes[j] = lane, ""
			}
			r.Above = append(r.Above, Edge{From: j, To: to, Color: colors[j]})
		}
		set(lane, "")
		fromNode := map[int]bool{}
		if len(c.Parents) > 0 {
			set(lane, c.Parents[0])
			fromNode[lane] = true
		}
		for _, p := range c.Parents[min(1, len(c.Parents)):] {
			k := slices.Index(lanes, p)
			if k < 0 {
				k = free()
				set(k, p)
				open(k)
			}
			fromNode[k] = true
		}
		for j, sha := range lanes {
			if sha == "" {
				continue
			}
			e := Edge{From: j, To: j, Color: colors[j], Open: !held(sha)}
			if fromNode[j] {
				r.Below = append(r.Below, Edge{From: lane, To: j, Color: e.Color, Open: e.Open})
			}
			if !fromNode[j] || (j != lane && j < len(before) && before[j] == sha) {
				r.Below = append(r.Below, e)
			}
		}
		for len(lanes) > 0 && lanes[len(lanes)-1] == "" {
			lanes = lanes[:len(lanes)-1]
		}
		r.Color = colors[lane]
		rows[i] = r
	}
	return rows
}

// width is the lanes rows draw over, at most maxLanes.
func width(rows []Row) int {
	w := 0
	for _, r := range rows {
		w = max(w, r.Lane+1)
		for _, e := range r.Above {
			w = max(w, e.From+1)
		}
		for _, e := range r.Below {
			w = max(w, e.To+1)
		}
	}
	return min(w, maxLanes)
}
