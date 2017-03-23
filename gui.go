package main

import (
	"fmt"
	"github.com/jroimartin/gocui"
	"log"
)

var view_rooms_w int = 24
var view_users_w int = 24
var view_timeline_w int = 22
var view_chat_min_w int = 26

var readline_h int = 1
var readline_max_h = 8

func main() {
	g, err := gocui.NewGui(gocui.OutputNormal)
	if err != nil {
		log.Panicln(err)
	}
	defer g.Close()

	g.SetManagerFunc(layout)

	if err := g.SetKeybinding("", gocui.KeyCtrlC, gocui.ModNone, quit); err != nil {
		log.Panicln(err)
	}

	if err := g.MainLoop(); err != nil && err != gocui.ErrQuit {
		log.Panicln(err)
	}
}

func layout(g *gocui.Gui) error {
	maxX, maxY := g.Size()
	if maxX < 2+view_rooms_w+view_timeline_w+view_chat_min_w+view_users_w || maxY < 16 {
		v, err := g.SetView("err_win", -1, -1, maxX, maxY)
		if err != nil {
			if err != gocui.ErrUnknownView {
				return err
			}
			fmt.Fprintln(v, "Window too small")
		}
		g.SetViewOnTop("err_win")
		return nil
	} else {
		g.DeleteView("err_win")
	}
	if v, err := g.SetView("rooms", -1, -1, view_rooms_w, maxY); err != nil {
		if err != gocui.ErrUnknownView {
			return err
		}
		v.Frame = true
		fmt.Fprintln(v, "    Favourites (1)")
		fmt.Fprintln(v, "")
		fmt.Fprintln(v, " 1.Criptica")
		fmt.Fprintln(v, "")
		fmt.Fprintln(v, "    People (3)")
		fmt.Fprintln(v, "")
		fmt.Fprintln(v, " 2.Johnny")
		fmt.Fprintln(v, " 3.Jack")
		fmt.Fprintln(v, " 4.Jane")
		fmt.Fprintln(v, "")
		fmt.Fprintln(v, "    Rooms (6)")
		fmt.Fprintln(v, "")
		fmt.Fprintln(v, " 5.#debian-reproducible")
		fmt.Fprintln(v, " 6.#reproducible-builds")
		fmt.Fprintln(v, " 7.#openbsd")
		fmt.Fprintln(v, " 8.#gbdev")
		fmt.Fprintln(v, " 9.#archlinux")
		fmt.Fprintln(v, "10.#rust")
	}
	if v, err := g.SetView("users", maxX-view_users_w, -1, maxX, maxY); err != nil {
		if err != gocui.ErrUnknownView {
			return err
		}
		v.Frame = true
		fmt.Fprintln(v, "@alice")
		fmt.Fprintln(v, "@bob")
		fmt.Fprintln(v, "@dhole")
		fmt.Fprintln(v, "eve")
		fmt.Fprintln(v, "mallory")
		fmt.Fprintln(v, "anon")
		fmt.Fprintln(v, "steve")
	}
	if v, err := g.SetView("readline", view_rooms_w, maxY-1-readline_h, maxX-view_users_w, maxY); err != nil {
		if err != gocui.ErrUnknownView {
			return err
		}
		v.Frame = false
		v.Editable = true
		fmt.Fprintln(v, "")
	}
	if v, err := g.SetView("statusline", view_rooms_w, maxY-2-readline_h, maxX-view_rooms_w, maxY-readline_h); err != nil {
		if err != gocui.ErrUnknownView {
			return err
		}
		v.Frame = false
		v.BgColor = gocui.ColorBlue
		fmt.Fprintln(v, "\x1b[0;37m[03:14] [@dhole:matrix.org(+)] 2:#debian-reproducible [6] {encrypted}")
	}
	if v, err := g.SetView("timeline", view_rooms_w, -1, view_rooms_w+view_timeline_w, maxY-1-readline_h); err != nil {
		if err != gocui.ErrUnknownView {
			return err
		}
		v.Frame = false
		fmt.Fprintln(v, "\x1b[0;33m01:40:56\x1b[0;36m      alice \x1b[0;35m| ")
		fmt.Fprintln(v, "\x1b[0;33m01:40:58\x1b[0;32m        bob \x1b[0;35m| ")
	}
	if v, err := g.SetView("chat", view_rooms_w+view_timeline_w, -1, maxX-view_rooms_w, maxY-1-readline_h); err != nil {
		if err != gocui.ErrUnknownView {
			return err
		}
		v.Frame = false
		v.Wrap = true
		fmt.Fprintln(v, "OLA K ASE")
		fmt.Fprintln(v, "OLA K DISE")
	}
	g.SetViewOnTop("statusline")
	g.SetCurrentView("readline")
	return nil
}

func quit(g *gocui.Gui, v *gocui.View) error {
	return gocui.ErrQuit
}
