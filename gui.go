package main

import (
	"bytes"
	"fmt"
	"github.com/jroimartin/gocui"
	"io"
	"log"
	"sort"
	"strings"
	"time"
)

type Message struct {
	Msgtype string
	Ts      int64
	UserID  string
	Body    string
}

type Membership int

const (
	MemInvite Membership = iota
	MemJoin   Membership = iota
	MemLeave  Membership = iota
)

type User struct {
	Id       string
	Name     string
	DispName string
	Power    int
	Mem      Membership
}

type Users struct {
	U      []*User
	ById   map[string]*User
	ByName map[string][]*User
}

type Room struct {
	Id              string
	Name            string
	DispName        string
	Alias           string
	Topic           string
	Fav             bool
	Users           Users
	Msgs            []Message
	ViewUsersBuf    *string
	ViewTimelineBuf *string
	ViewMsgsBuf     *string
}

type Rooms struct {
	R      []*Room
	ById   map[string]*Room
	ByName map[string][]*Room
}

type Words []string

// CONFIG

var view_rooms_w int = 24
var view_users_w int = 24
var view_timeline_w int = 22
var view_msgs_min_w int = 26

var window_w int = -1
var window_h int = -1

var readline_h int = 1

var display_names_id = false

// END CONFIG

//var readline_max_h = 8

var MyUserName = "dhole"
var MyUserID = "@dhole:matrix.org"

var currentRoom *Room
var rs Rooms

var debug_buf *bytes.Buffer

var ReadLineEditor gocui.Editor = gocui.EditorFunc(readLine)
var ReadMultiLineEditor gocui.Editor = gocui.EditorFunc(readMultiLine)

var readline_multiline bool
var readline_buff []Words
var readline_idx int

func (r *Room) String() string {
	return r.DispName
}

func (r *Room) UpdateDispName() {
	if r.Name != "" {
		r.DispName = r.Name
		return
	}
	if r.Alias != "" {
		r.DispName = r.Alias
		return
	}
	roomUserIds := make([]string, 0)
	for k := range r.Users.ById {
		if k == MyUserID {
			continue
		}
		roomUserIds = append(roomUserIds, k)
	}
	if len(roomUserIds) == 1 {
		r.DispName = r.Users.ById[roomUserIds[0]].String()
		return
	}
	sort.Strings(roomUserIds)
	if len(roomUserIds) == 2 {
		r.DispName = fmt.Sprint(r.Users.ById[roomUserIds[0]], "and",
			r.Users.ById[roomUserIds[1]])
		return
	}
	if len(roomUserIds) > 2 {
		r.DispName = fmt.Sprint(r.Users.ById[roomUserIds[0]], "and",
			len(roomUserIds)-1, "others")
		return
	}
	r.DispName = "Emtpy room"
}

func (u *User) String() string {
	if display_names_id {
		return u.Id
	} else {
		return u.DispName
	}
}

func (u *User) UpdateDispName(r *Room) {
	defer r.UpdateDispName()
	if u.Name == "" {
		u.DispName = u.Id
		return
	}
	if len(r.Users.ByName[u.Name]) > 1 {
		u.DispName = fmt.Sprintf("%s (%s)", u.Name, u.Id)
		return
	}
	u.DispName = u.Name
	return
}

func readLine(v *gocui.View, key gocui.Key, ch rune, mod gocui.Modifier) {
	switch {
	case ch != 0 && mod == 0:
		v.EditWrite(ch)
	case key == gocui.KeySpace:
		v.EditWrite(' ')
	case key == gocui.KeyBackspace || key == gocui.KeyBackspace2:
		v.EditDelete(true)
	case key == gocui.KeyEnter:
		body := v.Buffer()
		body = body[:len(body)-1]
		v.Clear()
		v.SetOrigin(0, 0)
		v.SetCursor(0, 0)
		if len(body) != 0 {
			sendMsg(body)
		}
	}
}

func readMultiLine(v *gocui.View, key gocui.Key, ch rune, mod gocui.Modifier) {
	// TODO
	readLine(v, key, ch, mod)
}

func initMsgs() {
	msgs := &rs.R[1].Msgs
	*msgs = append(*msgs, Message{"m.text", 1234, "@a:matrix.org", "OLA K ASE"})
	*msgs = append(*msgs, Message{"m.text", 1246, "@b:matrix.org", "OLA K DISE"})
	*msgs = append(*msgs, Message{"m.text", 1249, "@a:matrix.org", "Pos por ahi, con la moto"})
	*msgs = append(*msgs, Message{"m.text", 1249, "@foobar:matrix.org", "Andaaa, poh no me digas      mas  hehe     toma tomate"})
	*msgs = append(*msgs, Message{"m.text", 1258, "@a:matrix.org", "Lorem ipsum dolor sit amet, consectetur adipiscing elit. Proin eget diam egestas, sollicitudin sapien eu, gravida tortor. Vestibulum eu malesuada est, vitae blandit augue. Phasellus mauris nisl, cursus quis nunc ut, vulputate condimentum felis. Aenean ut arcu orci. Morbi eget tempor diam. Curabitur semper lorem a nisi sagittis blandit. Nam non urna ligula."})
	*msgs = append(*msgs, Message{"m.text", 1277, "@a:matrix.org", "Praesent pretium eu sapien sollicitudin blandit. Nullam lacinia est ut neque suscipit congue. In ullamcorper congue ornare. Donec lacus arcu, faucibus ut interdum eget, aliquet sed leo. Suspendisse eget congue massa, at ornare nunc. Cras ac est nunc. Morbi lacinia placerat varius. Cras imperdiet augue eu enim condimentum gravida nec nec est."})
	for i := int64(0); i < 120; i++ {
		*msgs = append(*msgs, Message{"m.text", 1278 + i, "@anon:matrix.org", fmt.Sprintf("msg #%3d", i)})
	}
}

func NewUsers() (us Users) {
	us.U = make([]*User, 0)
	us.ById = make(map[string]*User, 0)
	us.ByName = make(map[string][]*User, 0)
	return us
}

func (us *Users) Add(u *User) {
	us.U = append(us.U, u)
	us.ById[u.Id] = u
	if u.Name != "" {
		if us.ByName[u.Name] == nil {
			us.ByName[u.Name] = make([]*User, 0)
		}
		us.ByName[u.Name] = append(us.ByName[u.Name], u)
	}
	//fmt.Fprintf(debug_buf, "us:      %p\n", &us.U[len(us.U)-1])
	//fmt.Fprintf(debug_buf, "us.ById: %p\n", us.ById[u.Id])
	//fmt.Fprintf(debug_buf, "_u:      %p\n", _u)
}

func (us *Users) Del(u User) {
	// TODO
	// TODO: What if there are two users with the same name?
}

func (us *Users) SetUserName(u User, name string) {
	// TODO
	// TODO: What if there are two users with the same name?
}

func NewRoom(id string, name string, topic string) (r *Room) {
	r = &Room{}
	r.Id = id
	r.Name = name
	r.Topic = topic
	r.Users = NewUsers()
	r.Msgs = make([]Message, 0)
	return r
}

func NewRooms() (rs Rooms) {
	rs.R = make([]*Room, 0)
	rs.ById = make(map[string]*Room, 0)
	rs.ByName = make(map[string][]*Room, 0)
	return rs
}

func (rs *Rooms) Add(r *Room) {
	rs.R = append(rs.R, r)
	rs.ById[r.Id] = r
	if r.Name != "" {
		if rs.ByName[r.Name] == nil {
			rs.ByName[r.Name] = make([]*Room, 0)
		}
		rs.ByName[r.Name] = append(rs.ByName[r.Name], r)
	}
}

func (rs *Rooms) Del(r Room) {
	// TODO
	// TODO: What if there are two rooms with the same name?
}

func (rs *Rooms) SetRoomName(r Room, name string) {
	// TODO
	// TODO: What if there are two rooms with the same name?
}

func initRooms() {
	rs = NewRooms()
	rs.Add(NewRoom("!cZaiLMbuSWouYFGEDS:matrix.org", "", ""))
	rs.Add(NewRoom("!xAbiTnitnIIjlhlaWC:matrix.org", "Criptica", "Defensant la teva privacitat des de 1984"))
	currentRoom = rs.R[1]

	rs.R[0].Users.Add(&User{MyUserID, MyUserName, "", 0, MemJoin})
	rs.R[0].Users.Add(&User{"@a:matrix.org", "Alice", "", 0, MemJoin})

	rs.R[1].Users.Add(&User{MyUserID, MyUserName, "N", 100, MemJoin})
	rs.R[1].Users.Add(&User{"@a:matrix.org", "Alice", "", 100, MemJoin})
	rs.R[1].Users.Add(&User{"@b:matrix.org", "Bob", "", 100, MemJoin})
	rs.R[1].Users.Add(&User{"@e:matrix.org", "Eve", "", 0, MemJoin})
	rs.R[1].Users.Add(&User{"@m:matrix.org", "Mallory", "", 0, MemJoin})
	rs.R[1].Users.Add(&User{"@anon:matrix.org", "Anon", "", 0, MemJoin})
	rs.R[1].Users.Add(&User{"@steve1:matrix.org", "Steve", "", 0, MemJoin})
	rs.R[1].Users.Add(&User{"@steve2:matrix.org", "Steve", "", 0, MemJoin})
	rs.R[1].Users.Add(&User{"@foobar:matrix.org", "my_user_name_is_very_long", "", 0, MemJoin})

	for _, u := range rs.R[0].Users.U {
		u.UpdateDispName(rs.R[0])
	}
	for _, u := range rs.R[1].Users.U {
		u.UpdateDispName(rs.R[1])
	}
	fmt.Fprintln(debug_buf, rs.R[1].Users.U[0])
	rs.R[0].UpdateDispName()
	rs.R[1].UpdateDispName()
}

func initReadline() {
	readline_buff = make([]Words, 1)
	readline_idx = 0
	readline_multiline = false
}

func scrollChat(g *gocui.Gui, v *gocui.View, l int) error {
	v_msgs, err := g.View("msgs")
	if err != nil {
		return err
	}
	v_timeline, err := g.View("timeline")
	if err != nil {
		return err
	}
	x_c, y_c := v_msgs.Origin()
	x_t, y_t := v_timeline.Origin()
	v_msgs.SetOrigin(x_c, y_c+l)
	v_timeline.SetOrigin(x_t, y_t+l)
	return nil
}

func main() {
	debug_buf = bytes.NewBufferString("")
	initRooms()
	initMsgs()
	initReadline()
	g, err := gocui.NewGui(gocui.OutputNormal)
	if err != nil {
		log.Panicln(err)
	}
	defer g.Close()

	g.SetManagerFunc(layout)
	g.Cursor = true

	if err := g.SetKeybinding("", gocui.KeyCtrlC, gocui.ModNone, quit); err != nil {
		log.Panicln(err)
	}
	if err := g.SetKeybinding("", gocui.KeyF7, gocui.ModNone, keyDebugToggle); err != nil {
		log.Panicln(err)
	}
	if err := g.SetKeybinding("", gocui.KeyF6, gocui.ModNone, keyReadmultiLineToggle); err != nil {
		log.Panicln(err)
	}
	if err := g.SetKeybinding("", gocui.KeyPgup, gocui.ModNone,
		func(g *gocui.Gui, v *gocui.View) error { return scrollChat(g, v, -1) }); err != nil {
		log.Panicln(err)
	}
	if err := g.SetKeybinding("", gocui.KeyPgdn, gocui.ModNone,
		func(g *gocui.Gui, v *gocui.View) error { return scrollChat(g, v, 1) }); err != nil {
		log.Panicln(err)
	}
	// TODO
	// F11 / F12: scroll nicklist
	// F9 / F10: scroll roomlist
	// PgUp / PgDn: scroll text in current buffer

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
		window_w = maxX
		window_h = maxY
		win_new_size = true
	}
	if win_new_size {
		if maxX < 2+view_rooms_w+view_timeline_w+view_msgs_min_w+view_users_w || maxY < 16 {
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
		fmt.Fprintln(v, " my_user_name_is_very_long")
	}
	if v, err := g.SetView("readline", view_rooms_w, maxY-1-readline_h, maxX-view_users_w, maxY); err != nil {
		if err != gocui.ErrUnknownView {
			return err
		}
		if err == gocui.ErrUnknownView {
			v.Frame = false
			v.Editable = true
			v.Editor = ReadLineEditor
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
	v_msgs, err := g.SetView("msgs", view_rooms_w+view_timeline_w+1, -1, maxX-view_rooms_w, maxY-1-readline_h)
	if err != nil {
		if err != gocui.ErrUnknownView {
			return err
		}
		if err == gocui.ErrUnknownView {
			v_msgs.Frame = false
			v_msgs.Wrap = true
		}
		//fmt.Fprintln(v, "OLA K ASE")
		//fmt.Fprintln(v, "OLA K DISE")
	}
	if win_new_size {
		v_msgs.Clear()
		v_timeline.Clear()
		//debug_buf.Reset()
		fmt.Fprintln(debug_buf, "New Size at", time.Now())
		v_msgs_w, _ := v_msgs.Size()
		//fmt.Fprintln(v_debug, "v_msgs_w =", v_msgs_w)
		for _, m := range currentRoom.Msgs {
			printMessage(v_timeline, v_msgs, v_msgs_w, &m, currentRoom)
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

func printMessage(viewTimeline io.Writer, viewMsg io.Writer, msgWidth int, m *Message, r *Room) {
	t := time.Unix(m.Ts, 0)
	user := r.Users.ById[m.UserID]
	//displayName := ""
	//if user.Power >= 50 {
	//	displayName = fmt.Sprintf("@%s", user)
	//} else {
	//	displayName = fmt.Sprintf("%s", user)
	//}
	fmt.Fprintln(debug_buf, r.Users.U[1])
	fmt.Fprintln(debug_buf, r.Users.ById[user.Id])
	fmt.Fprintln(viewTimeline, t.Format("15:04:05"), StrPad(user.String(), view_timeline_w-11), "|")
	lines := 1
	s_len := 0
	for i, w := range strings.Split(m.Body, " ") {
		if s_len+len(w)+1 > msgWidth {
			if s_len != 0 {
				fmt.Fprint(viewMsg, "\n")
				lines += 1
				s_len = 0
			}
			fmt.Fprint(viewMsg, w)
			lines += int(len(w) / msgWidth)
			s_len += len(w) % msgWidth
		} else {
			if i != 0 {
				fmt.Fprint(viewMsg, " ")
				s_len += 1
			}
			fmt.Fprint(viewMsg, w)
			s_len += len(w)
		}
	}
	fmt.Fprint(viewMsg, "\n")
	if lines > 1 {
		//fmt.Fprintln(debug_buf, "Extra newline in v_timeline")
		fmt.Fprint(viewTimeline, strings.Repeat(StrPad("|", view_timeline_w)+"\n", lines-1))
	}
}

func quit(g *gocui.Gui, v *gocui.View) error {
	return gocui.ErrQuit
}

func keyDebugToggle(g *gocui.Gui, v *gocui.View) error {
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
			v_debug.Wrap = true
		}
		fmt.Fprint(v_debug, debug_buf)
	}
	return nil
}

func sendMsg(body string) error {
	msg := Message{"m.text", time.Now().Unix(), MyUserName, body}
	currentRoom.Msgs = append(currentRoom.Msgs, msg)
	return nil
}

func keyReadmultiLineToggle(g *gocui.Gui, v *gocui.View) error {
	v_readline, err := g.View("readline")
	if err != nil {
		return err
	}
	if readline_multiline {
		readline_multiline = false
		readline_h = 1
		v_readline.Editor = ReadLineEditor
	} else {
		readline_multiline = true
		readline_h = 5
		v_readline.Editor = ReadMultiLineEditor
	}
	return nil
}
