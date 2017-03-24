package main

import (
	"bytes"
	"fmt"
	"github.com/jroimartin/gocui"
	"log"
	"strings"
	"time"
)

type Message struct {
	msgtype string
	ts      int64
	user    string
	body    string
}

var view_rooms_w int = 24
var view_users_w int = 24
var view_timeline_w int = 22
var view_chat_min_w int = 26

var window_w int = -1
var window_h int = -1

var readline_h int = 1
var readline_max_h = 8

var my_nick = "dhole"

var msgs []Message

var debug_buf *bytes.Buffer

func init_msgs() {
	msgs = make([]Message, 0)
	msgs = append(msgs, Message{"m.text", 1234, "alice", "OLA K ASE"})
	msgs = append(msgs, Message{"m.text", 1246, "bob", "OLA K DISE"})
	msgs = append(msgs, Message{"m.text", 1249, "alice", "Pos por ahi, con la moto"})
	msgs = append(msgs, Message{"m.text", 1249, "bob", "Andaaa, poh no me digas      mas  hehe     toma tomate"})
	msgs = append(msgs, Message{"m.text", 1258, "alice", "Lorem ipsum dolor sit amet, consectetur adipiscing elit. Proin eget diam egestas, sollicitudin sapien eu, gravida tortor. Vestibulum eu malesuada est, vitae blandit augue. Phasellus mauris nisl, cursus quis nunc ut, vulputate condimentum felis. Aenean ut arcu orci. Morbi eget tempor diam. Curabitur semper lorem a nisi sagittis blandit. Nam non urna ligula."})
	msgs = append(msgs, Message{"m.text", 1277, "alice", "Praesent pretium eu sapien sollicitudin blandit. Nullam lacinia est ut neque suscipit congue. In ullamcorper congue ornare. Donec lacus arcu, faucibus ut interdum eget, aliquet sed leo. Suspendisse eget congue massa, at ornare nunc. Cras ac est nunc. Morbi lacinia placerat varius. Cras imperdiet augue eu enim condimentum gravida nec nec est."})
}

func main() {
	init_msgs()
	debug_buf = bytes.NewBufferString("")
	g, err := gocui.NewGui(gocui.OutputNormal)
	if err != nil {
		log.Panicln(err)
	}
	defer g.Close()

	g.SetManagerFunc(layout)

	if err := g.SetKeybinding("", gocui.KeyCtrlC, gocui.ModNone, quit); err != nil {
		log.Panicln(err)
	}
	if err := g.SetKeybinding("", gocui.KeyF6, gocui.ModNone, key_debug_toggle); err != nil {
		log.Panicln(err)
	}
	if err := g.SetKeybinding("", gocui.KeyEnter, gocui.ModNone, key_send_msg); err != nil {
		log.Panicln(err)
	}

	if err := g.MainLoop(); err != nil && err != gocui.ErrQuit {
		log.Panicln(err)
	}
}

func StrPad(s string, pLen int) string {
	if len(s) > pLen-2 {
		return s[:pLen-2] + ".."
	} else {
		return strings.Repeat(" ", pLen-len(s)) + s
	}
}

func layout(g *gocui.Gui) error {
	maxX, maxY := g.Size()
	win_new_size := false
	if window_w != maxX || window_h != maxY {
		window_h = maxX
		window_w = maxY
		win_new_size = true
	}
	if win_new_size {
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
	}
	if v, err := g.SetView("rooms", -1, -1, view_rooms_w, maxY); err != nil {
		if err != gocui.ErrUnknownView {
			return err
		}
		if err == gocui.ErrUnknownView {
			v.Frame = true
		}
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
		if err == gocui.ErrUnknownView {
			v.Frame = true
		}
		fmt.Fprintln(v, "@alice")
		fmt.Fprintln(v, "@bob")
		fmt.Fprintln(v, "@dhole")
		fmt.Fprintln(v, " eve")
		fmt.Fprintln(v, " mallory")
		fmt.Fprintln(v, " anon")
		fmt.Fprintln(v, " steve")
	}
	if v, err := g.SetView("readline", view_rooms_w, maxY-1-readline_h, maxX-view_users_w, maxY); err != nil {
		if err != gocui.ErrUnknownView {
			return err
		}
		if err == gocui.ErrUnknownView {
			v.Frame = false
			v.Editable = true
		}
		fmt.Fprintln(v, "")
	}
	if v, err := g.SetView("statusline", view_rooms_w, maxY-2-readline_h, maxX-view_rooms_w, maxY-readline_h); err != nil {
		if err != gocui.ErrUnknownView {
			return err
		}
		if err == gocui.ErrUnknownView {
			v.Frame = false
			v.BgColor = gocui.ColorBlue
		}
		fmt.Fprintln(v, "\x1b[0;37m[03:14] [@dhole:matrix.org(+)] 2:#debian-reproducible [6] {encrypted}")
	}
	v_timeline, err := g.SetView("timeline", view_rooms_w, -1, view_rooms_w+view_timeline_w+1, maxY-1-readline_h)
	if err != nil {
		if err != gocui.ErrUnknownView {
			return err
		}
		if err == gocui.ErrUnknownView {
			v_timeline.Frame = false
		}
		//fmt.Fprintln(v, "\x1b[0;33m01:40:56\x1b[0;36m      alice \x1b[0;35m| ")
		//fmt.Fprintln(v, "\x1b[0;33m01:40:58\x1b[0;32m        bob \x1b[0;35m| ")
	}
	v_chat, err := g.SetView("chat", view_rooms_w+view_timeline_w+1, -1, maxX-view_rooms_w, maxY-1-readline_h)
	if err != nil {
		if err != gocui.ErrUnknownView {
			return err
		}
		if err == gocui.ErrUnknownView {
			v_chat.Frame = false
			v_chat.Wrap = true
		}
		//fmt.Fprintln(v, "OLA K ASE")
		//fmt.Fprintln(v, "OLA K DISE")
	}
	if win_new_size {
		v_chat.Clear()
		v_timeline.Clear()
		debug_buf.Reset()
		v_chat_w, _ := v_chat.Size()
		//fmt.Fprintln(v_debug, "v_chat_w =", v_chat_w)
		for _, m := range msgs {
			t := time.Unix(m.ts, 0)
			fmt.Fprintln(v_timeline, t.Format("15:04:05"), StrPad(m.user, view_timeline_w-11), "|")
			lines := 1
			s_len := 0
			for i, w := range strings.Split(m.body, " ") {
				if s_len+len(w)+1 > v_chat_w {
					if s_len != 0 {
						fmt.Fprint(v_chat, "\n")
						lines += 1
						s_len = 0
					}
					fmt.Fprint(v_chat, w)
					lines += int(len(w) / v_chat_w)
					s_len += len(w) % v_chat_w
				} else {
					if i != 0 {
						fmt.Fprint(v_chat, " ")
						s_len += 1
					}
					fmt.Fprint(v_chat, w)
					s_len += len(w)
				}
			}
			fmt.Fprint(v_chat, "\n")
			if lines > 1 {
				fmt.Fprintln(debug_buf, "Extra newline in v_timeline")
				fmt.Fprint(v_timeline, strings.Repeat(StrPad("|", view_timeline_w)+"\n", lines-1))
			}
		}
	}
	if _, err := g.View("debug"); err == nil {
		g.SetView("debug", maxX/2, maxY/2, maxX, maxY)
	}

	g.SetViewOnTop("statusline")
	g.SetViewOnTop("debug")
	g.SetCurrentView("readline")
	return nil
}

func quit(g *gocui.Gui, v *gocui.View) error {
	return gocui.ErrQuit
}

func key_debug_toggle(g *gocui.Gui, v *gocui.View) error {
	maxX, maxY := g.Size()
	if _, err := g.View("debug"); err == nil {
		g.DeleteView("debug")
		return nil
	}
	v_debug, err := g.SetView("debug", maxX/2, maxY/2, maxX, maxY)
	if err != nil {
		if err != gocui.ErrUnknownView {
			return err
		}
		if err == gocui.ErrUnknownView {
			v_debug.Frame = true
			v_debug.Title = "Debug"
		}
		fmt.Fprint(v_debug, debug_buf)
	}
	return nil
}

func key_send_msg(g *gocui.Gui, v *gocui.View) error {
	v_readline, err := g.View("readline")
	if err != nil {
		return err
	}
	body := v_readline.Buffer()
	v_readline.Clear()
	v_readline.SetOrigin(0, 0)
	v_readline.SetCursor(0, 0)
	if len(body) == 0 {
		return nil
	}
	msg := Message{"m.text", time.Now().Unix(), my_nick, body[:len(body)-1]}
	msgs = append(msgs, msg)
	return nil
}
