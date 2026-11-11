// Command plot draws the README charts from numbers in RESULTS.md
package main

import (
	"flag"
	"log"
	"path/filepath"
	"strconv"

	"gonum.org/v1/plot"
	"gonum.org/v1/plot/plotter"
	"gonum.org/v1/plot/plotutil"
	"gonum.org/v1/plot/vg"
)

// series is one line on a chart, points are orders per second and p99 in ms
type series struct {
	name string
	pts  [][2]float64
}

var limits = []series{
	{"наивный, 1 горутина", [][2]float64{
		{10, 23.3}, {20, 23.9}, {30, 23.6}, {40, 23.3}, {50, 23.1}, {60, 25.4}, {61, 25.4},
		{62, 79.7}, {63, 466.9}, {64, 1065.0}, {65, 1547.3}, {66, 2001.9}, {67, 2467.8},
		{68, 2906.1}, {69, 3444.7}, {70, 3813.4},
	}},
	{"сетка 500 m, 1 горутина", [][2]float64{
		{40000, 0.022}, {60000, 1.358}, {64000, 1.454}, {68000, 1.471}, {72000, 1.599},
		{76000, 2.279}, {80000, 3.205}, {90000, 1236.991},
	}},
	{"сетка 200 m, 4 горутины, пачка 10", [][2]float64{
		{300000, 0.017}, {400000, 0.048}, {500000, 0.061}, {600000, 0.210}, {800000, 0.587},
		{1000000, 25.727},
	}},
}

var locks = []series{
	{"Mutex, 8 горутин, пачка 1", [][2]float64{{50000, 0.038}, {100000, 0.041}, {200000, 0.561}, {300000, 13.951}}},
	{"RWMutex, 8 горутин, пачка 1", [][2]float64{{50000, 0.028}, {100000, 0.037}, {200000, 0.041}, {300000, 0.701}}},
	{"владелец + канал, 8 горутин, пачка 1", [][2]float64{{50000, 0.046}, {100000, 0.041}, {200000, 0.294}, {300000, 2260.991}}},
	{"8 шардов по ID, 8 горутин, пачка 1", [][2]float64{{50000, 0.104}, {100000, 1.082}, {200000, 17711.103}, {300000, 41091.071}}},
	{"RWMutex, 4 горутины, пачка 10", [][2]float64{{50000, 0.012}, {200000, 0.012}, {300000, 0.024}}},
}

func main() {
	out := flag.String("out", ".", "directory for svg files")
	flag.Parse()

	save(filepath.Join(*out, "limits.svg"), "Где ломается каждый шаг", true, false, limits)
	save(filepath.Join(*out, "locks.svg"), "Сетка 200 m под нагрузкой из нескольких горутин", false, true, locks)
}

func save(path, title string, logX, legendTop bool, ss []series) {
	p := plot.New()
	p.Title.Text = title
	p.X.Label.Text = "заказов в секунду"
	p.Y.Label.Text = "p99, ms"
	p.Y.Scale = plot.LogScale{}
	p.Y.Tick.Marker = plainTicks{}
	p.Y.Min = 0.01
	if logX {
		p.X.Scale = plot.LogScale{}
		p.X.Tick.Marker = plainTicks{}
	}
	p.Add(plotter.NewGrid())
	p.Legend.Top = legendTop
	if legendTop {
		// room above the lines for the legend
		p.Y.Max = 3e5
	}
	p.Legend.Left = true

	var args []any
	for _, s := range ss {
		xys := make(plotter.XYs, len(s.pts))
		for i, pt := range s.pts {
			xys[i].X, xys[i].Y = pt[0], pt[1]
		}
		args = append(args, s.name, xys)
	}
	if err := plotutil.AddLinePoints(p, args...); err != nil {
		log.Fatal(err)
	}
	if err := p.Save(18*vg.Centimeter, 11*vg.Centimeter, path); err != nil {
		log.Fatal(err)
	}
}

// plainTicks are log ticks labeled 1000000 instead of 1e+06
type plainTicks struct{}

func (plainTicks) Ticks(min, max float64) []plot.Tick {
	ts := plot.LogTicks{Prec: -1}.Ticks(min, max)
	for i := range ts {
		if ts[i].Label != "" {
			ts[i].Label = strconv.FormatFloat(ts[i].Value, 'f', -1, 64)
		}
	}
	return ts
}
