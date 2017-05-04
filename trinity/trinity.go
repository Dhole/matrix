package trinity

import (
	"../list"
	"bytes"
	"fmt"
	//"github.com/jroimartin/gocui"
	"../../gocui"
	"hash/adler32"
	//"io"
	"log"
	"sort"
	"strings"
	"time"
)

type RGBColor struct {
	r, g, b byte
}

type Message struct {
	MsgType string
	Ts      int64
	UserId  string
	Body    string
}

type Membership int

const (
	MemInvite Membership = iota
	MemJoin   Membership = iota
	MemLeave  Membership = iota
)

const (
	ConsoleUserId   string = "@trinity:localhost"
	ConsoleUsername string = "trinity"
	ConsoleRoomId   string = "!console:localhost"
)

type User struct {
	Id           string
	Name         string
	DispName     string
	DispNameHash uint32
	Power        int
	Mem          Membership
}

type Users struct {
	U      []*User
	ById   map[string]*User
	ByName map[string][]*User
}

type Room struct {
	Id       string
	Name     string
	DispName string
	Alias    string
	Topic    string
	Shortcut int
	Fav      bool
	Users    Users
	//Msgs         []Message
	Msgs *list.List
	//ViewUsersBuf   *string
	//ViewMsgsBuf    *string
	ViewMsgsOriginY     int
	ScrollBottom        bool
	ViewReadlineBuf     string
	ViewReadlineCursorX int
}

type Rooms struct {
	R           []*Room
	ById        map[string]*Room
	ByName      map[string][]*Room
	ByShortcut  map[int]*Room
	PeopleRooms []*Room
	GroupRooms  []*Room
	ConsoleRoom *Room
}

type MessageRoom struct {
	Message *Message
	Room    *Room
}

type Words []string

// CONFIG

var nickRGBColors []RGBColor = []RGBColor{RGBColor{255, 89, 89}, RGBColor{255, 138, 89}, RGBColor{255, 188, 89}, RGBColor{255, 238, 89}, RGBColor{221, 255, 89}, RGBColor{172, 255, 89}, RGBColor{122, 255, 89}, RGBColor{89, 255, 105}, RGBColor{89, 255, 155}, RGBColor{89, 255, 205}, RGBColor{89, 255, 255}, RGBColor{89, 205, 255}, RGBColor{89, 155, 255}, RGBColor{89, 105, 255}, RGBColor{122, 89, 255}, RGBColor{172, 89, 255}, RGBColor{221, 89, 255}, RGBColor{255, 89, 238}, RGBColor{255, 89, 188}, RGBColor{255, 89, 138}}

var nick256Colors []byte = []byte{39, 51, 37, 42, 47, 82, 76, 70, 69, 96, 102, 105, 126, 109, 116, 120, 155, 149, 142, 136, 135, 141, 166, 183, 184, 191, 226, 220, 214, 208}

var viewRoomsWidth int = 24
var viewUsersWidth int = 22
var timelineWidth int = 8
var timelineUserWidth int = timelineWidth + 2 + 16
var viewMsgsMinWidth int = 26

var windowWidth int = -1
var windowHeight int = -1

var readlineHeight int = 1

var displayNamesId = false

// END CONFIG

// GLOBALS

var myUsername string
var myUserId string

var currentRoom *Room
var lastRoom *Room
var rs Rooms

var debugBuf *bytes.Buffer

var readLineEditor gocui.Editor = gocui.EditorFunc(readLine)
var readMultiLineEditor gocui.Editor = gocui.EditorFunc(readMultiLine)

var readlineMultiline bool
var readlineBuf []Words
var readlineIdx int

var viewMsgsHeight int
var viewMsgsLines int
var scrollBottom bool

// eventLoop channels
var sentMsgsChan chan MessageRoom
var recvMsgsChan chan MessageRoom
var rePrintChan chan string
var switchRoomChan chan bool
var cmdChan chan []string

var started bool

// END GLOBALS

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
		if k == myUserId {
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
	if displayNamesId {
		return u.Id
	} else {
		return u.DispName
	}
}

func (u *User) UpdateDispName(r *Room) {
	defer r.UpdateDispName()
	defer func() {
		u.DispNameHash = adler32.Checksum([]byte(u.DispName))
	}()
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

func min(x, y int) int {
	if x < y {
		return x
	}
	return y
}

func max(x, y int) int {
	if x > y {
		return x
	}
	return y
}

var modeLongRoomShortcut bool
var longRoomShortcutDec int = -1

func shortcuts(key gocui.Key, ch rune, mod gocui.Modifier) bool {
	roomShortcut := -1
	if modeLongRoomShortcut {
		switch {
		case ch >= '0' && ch <= '9':
			if longRoomShortcutDec == -1 {
				longRoomShortcutDec = int(ch - '0')
			} else {
				roomShortcut = int(ch-'0') + longRoomShortcutDec*10
				longRoomShortcutDec = -1
				modeLongRoomShortcut = false
			}
		default:
			longRoomShortcutDec = -1
			modeLongRoomShortcut = false
		}
	} else {
		switch {
		case mod == gocui.ModAlt && ch == '0':
			roomShortcut = 0
		case mod == gocui.ModAlt && ch == '1':
			roomShortcut = 1
		case mod == gocui.ModAlt && ch == '2':
			roomShortcut = 2
		case mod == gocui.ModAlt && ch == '3':
			roomShortcut = 3
		case mod == gocui.ModAlt && ch == '4':
			roomShortcut = 4
		case mod == gocui.ModAlt && ch == '5':
			roomShortcut = 5
		case mod == gocui.ModAlt && ch == '6':
			roomShortcut = 6
		case mod == gocui.ModAlt && ch == '7':
			roomShortcut = 7
		case mod == gocui.ModAlt && ch == '8':
			roomShortcut = 8
		case mod == gocui.ModAlt && ch == '9':
			roomShortcut = 9
		case mod == gocui.ModAlt && ch == 'j':
			modeLongRoomShortcut = true
		default:
			return false
		}
	}
	if roomShortcut != -1 {
		r, ok := rs.ByShortcut[roomShortcut]
		if ok {
			setCurrentRoom(r, true)
		}
	}
	return true
}

func readLine(v *gocui.View, key gocui.Key, ch rune, mod gocui.Modifier) {
	if shortcuts(key, ch, mod) {
		return
	}
	switch {
	case ch != 0 && mod == 0:
		v.EditWrite(ch)
	case key == gocui.KeySpace:
		v.EditWrite(' ')
	case key == gocui.KeyBackspace || key == gocui.KeyBackspace2:
		v.EditDelete(true)
	//case key == gocui.KeyCtrlU:
	//	v.Clear()
	//	v.SetOrigin(0, 0)
	//	v.SetCursor(0, 0)
	case key == gocui.KeyEnter:
		body := v.Buffer()
		if len(body) == 0 {
			return
		}
		body = body[:len(body)-1]
		v.Clear()
		v.SetOrigin(0, 0)
		v.SetCursor(0, 0)
		sendMsg(body, currentRoom)
	}
}

func readMultiLine(v *gocui.View, key gocui.Key, ch rune, mod gocui.Modifier) {
	// TODO
	readLine(v, key, ch, mod)
}

func NewUsers() (us Users) {
	us.U = make([]*User, 0)
	us.ById = make(map[string]*User, 0)
	us.ByName = make(map[string][]*User, 0)
	return us
}

func (us *Users) Add(id, name string, power int, mem Membership) {
	u := &User{
		Id:           id,
		Name:         name,
		DispName:     "",
		DispNameHash: 0,
		Power:        power,
		Mem:          mem,
	}
	us.U = append(us.U, u)
	us.ById[u.Id] = u
	if u.Name != "" {
		if us.ByName[u.Name] == nil {
			us.ByName[u.Name] = make([]*User, 0)
		}
		us.ByName[u.Name] = append(us.ByName[u.Name], u)
	}
	//fmt.Fprintf(debugBuf, "us:      %p\n", &us.U[len(us.U)-1])
	//fmt.Fprintf(debugBuf, "us.ById: %p\n", us.ById[u.Id])
	//fmt.Fprintf(debugBuf, "_u:      %p\n", _u)
}

func (us *Users) Del(u User) {
	// TODO
	// TODO: What if there are two users with the same name?
}

func (us *Users) SetUserName(u User, name string) {
	// TODO
	// TODO: What if there are two users with the same name?
}

func NewRoom(id, name, topic string) (r *Room) {
	r = &Room{}
	r.Id = id
	r.Name = name
	r.Topic = topic
	r.Users = NewUsers()
	//r.Msgs = make([]Message, 0)
	r.Msgs = list.New()
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

func (rs *Rooms) UpdateShortcuts() {
	rs.ByShortcut = make(map[int]*Room)
	rs.PeopleRooms = make([]*Room, 0)
	rs.GroupRooms = make([]*Room, 0)
	count := 0
	rs.ByShortcut[0] = rs.ConsoleRoom
	for _, r := range rs.R[1:] {
		if len(r.Users.U) == 2 {
			count++
			r.Shortcut = count
			rs.PeopleRooms = append(rs.PeopleRooms, r)
			rs.ByShortcut[r.Shortcut] = r
		}
	}
	for _, r := range rs.R[1:] {
		if len(r.Users.U) != 2 {
			count++
			r.Shortcut = count
			rs.GroupRooms = append(rs.GroupRooms, r)
			rs.ByShortcut[r.Shortcut] = r
		}
	}
}

func SetMyUsername(username string) {
	myUsername = username
}

func SetMyUserId(userId string) {
	myUserId = userId
}

func AddRoom(roomId, name, topic string) error {
	_, ok := rs.ById[roomId]
	if ok {
		return fmt.Errorf("Room %v already exists", roomId)
	}
	r := NewRoom(roomId, name, topic)
	r.UpdateDispName()
	rs.Add(r)
	rs.UpdateShortcuts()
	if started {
		if r == currentRoom {
			rePrintChan <- "rooms"
		}
	}
	return nil
}

func AddUser(roomId, userId, username string, power int, membership Membership) error {
	r, ok := rs.ById[roomId]
	if !ok {
		return fmt.Errorf("Room %v doesn't exist", roomId)
	}
	r.Users.Add(userId, username, power, membership)
	for _, u := range r.Users.U {
		u.UpdateDispName(r)
	}
	r.UpdateDispName()
	if started {
		if r == currentRoom {
			rePrintChan <- "users"
			rePrintChan <- "statusline"
		}
	}
	return nil
}

func AddMessage(roomId, msgType string, ts int64, userId, body string) error {
	r, ok := rs.ById[roomId]
	if !ok {
		return fmt.Errorf("Room %v doesn't exist", roomId)
	}
	_, ok = r.Users.ById[userId]
	if !ok {
		return fmt.Errorf("User %v doesn't exist in room %v", userId, roomId)
	}
	m := Message{msgType, ts, userId, body}
	if started {
		recvMsgsChan <- MessageRoom{&m, r}
	} else {
		r.Msgs.PushBack(m)
	}

	return nil
}

func initReadline() {
	readlineBuf = make([]Words, 1)
	readlineIdx = 0
	readlineMultiline = false
}

func scrollViewMsgs(g *gocui.Gui, l int) error {
	viewMsgs, err := g.View("msgs")
	if err != nil {
		return err
	}
	_, y := viewMsgs.Origin()
	newY := 0
	if l < 0 {
		newY = max(y+l, 0)
	}
	if l > 0 {
		newY = min(y+l, viewMsgsLines-viewMsgsHeight)
		newY = max(newY, 0)
	}
	viewMsgs.SetOrigin(0, newY)
	if newY >= viewMsgsLines-viewMsgsHeight {
		scrollBottom = true
	} else {
		scrollBottom = false
	}
	return nil
}

func scrollViewMsgsBottom(g *gocui.Gui) error {
	return scrollViewMsgs(g, viewMsgsLines-viewMsgsHeight)
}

func appendRoomMsg(g *gocui.Gui, r *Room, m *Message) {
	r.Msgs.PushBack(*m)
	g.Execute(func(g *gocui.Gui) error {
		viewMsgs, _ := g.View("msgs")
		if r == currentRoom {
			printMessage(viewMsgs, m, r)
			if scrollBottom {
				scrollViewMsgsBottom(g)
			}
		}
		return nil
	})
}

func printView(g *gocui.Gui, view string) {
	views := []string{view}
	if view == "all" {
		views = []string{"rooms", "msgs", "users",
			"readline", "statusline"}
	}
	for _, view := range views {
		switch view {
		case "rooms":
			v, _ := g.View(view)
			printRooms(v)
		case "msgs":
			v, _ := g.View(view)
			printRoomMessages(v, currentRoom)
		case "users":
			v, _ := g.View(view)
			printRoomUsers(v, currentRoom)
		case "readline":
			// TODO
			//v, _ := g.View(view)
			//printStatusLine(v, currentRoom)
		case "statusline":
			v, _ := g.View(view)
			printStatusLine(v, currentRoom)
		}
	}
}

func eventLoop(g *gocui.Gui) {
	for {
		select {
		case mr := <-sentMsgsChan:
			var r *Room
			if len(mr.Message.Body) >= 1 && mr.Message.Body[0] == '/' {
				r = rs.ConsoleRoom
			} else {
				r = mr.Room
			}
			appendRoomMsg(g, r, mr.Message)
			if r == rs.ConsoleRoom {
				body := strings.TrimPrefix(mr.Message.Body, "/")
				args := strings.Fields(body)
				//fmt.Fprint(debugBuf, args)
				cmdChan <- args
			}
		case mr := <-recvMsgsChan:
			appendRoomMsg(g, mr.Room, mr.Message)
		case view := <-rePrintChan:
			g.Execute(func(g *gocui.Gui) error {
				printView(g, view)
				return nil
			})
		case <-switchRoomChan:
			g.Execute(func(g *gocui.Gui) error {
				viewMsgs, _ := g.View("msgs")
				_, y := viewMsgs.Origin()
				lastRoom.ViewMsgsOriginY = y
				viewMsgs.SetOrigin(0, currentRoom.ViewMsgsOriginY)

				lastRoom.ScrollBottom = scrollBottom
				scrollBottom = currentRoom.ScrollBottom

				viewReadline, _ := g.View("readline")
				x, _ := viewReadline.Cursor()
				lastRoom.ViewReadlineBuf = viewReadline.Buffer()
				lastRoom.ViewReadlineCursorX = x
				viewReadline.Clear()
				viewReadline.SetOrigin(0, 0)
				viewReadline.Write([]byte(currentRoom.ViewReadlineBuf))
				viewReadline.SetCursor(currentRoom.ViewReadlineCursorX, 0)

				printView(g, "all")

				if scrollBottom {
					scrollViewMsgsBottom(g)
				}
				return nil

			})
		}

	}
}

func cmdLoop(g *gocui.Gui) {
	for {
		args := <-cmdChan
		if len(args) < 1 {
			continue
		}
		switch args[0] {
		case "quit":
			g.Execute(quit)
		default:
			consoleReply(fmt.Sprintf("Unknown command: %s", args[0]))
		}
	}
}

func consoleReply(rep string) {
	recvMsgsChan <- MessageRoom{
		&Message{"m.text", time.Now().Unix(), ConsoleUserId, rep},
		rs.ConsoleRoom}
}

func Init() {
	rs = NewRooms()
	AddRoom(ConsoleRoomId, "Console", "")
	AddUser(ConsoleRoomId, ConsoleUserId, ConsoleUsername, 100, MemJoin)
	rs.ConsoleRoom = rs.ById[ConsoleRoomId]
	if rs.ConsoleRoom != rs.R[0] {
		panic("ConsoleRoom is not rs.R[0]")
	}
	rs.ConsoleRoom.Shortcut = 0
	currentRoom = rs.ConsoleRoom
	lastRoom = currentRoom
	//currentRoom = rs.ConsoleRoom
}

func Start() error {
	debugBuf = bytes.NewBufferString("")
	lastRoom = currentRoom
	if myUserId == "" {
		return fmt.Errorf("UserId not set")
	}
	AddUser(ConsoleRoomId, myUserId, myUsername, 0, MemJoin)
	//initRooms()
	//initMsgs()
	initReadline()
	g, err := gocui.NewGui(gocui.Output256)
	//g, err := gocui.NewGui(gocui.Output)
	if err != nil {
		log.Panicln(err)
	}
	defer g.Close()

	g.SetManagerFunc(layout)
	g.Cursor = true

	if err := g.SetKeybinding("", gocui.KeyCtrlC, gocui.ModNone,
		func(g *gocui.Gui, v *gocui.View) error {
			return quit(g)
		}); err != nil {
		log.Panicln(err)
	}
	if err := g.SetKeybinding("", gocui.KeyF7, gocui.ModNone,
		keyDebugToggle); err != nil {
		log.Panicln(err)
	}
	if err := g.SetKeybinding("", gocui.KeyF6, gocui.ModNone,
		keyReadmultiLineToggle); err != nil {
		log.Panicln(err)
	}
	if err := g.SetKeybinding("", gocui.KeyArrowUp, gocui.ModNone,
		func(g *gocui.Gui, v *gocui.View) error {
			return scrollViewMsgs(g, -1)
		}); err != nil {
		log.Panicln(err)
	}
	if err := g.SetKeybinding("", gocui.KeyArrowDown, gocui.ModNone,
		func(g *gocui.Gui, v *gocui.View) error {
			return scrollViewMsgs(g, 1)
		}); err != nil {
		log.Panicln(err)
	}
	if err := g.SetKeybinding("", gocui.KeyCtrlU, gocui.ModNone,
		func(g *gocui.Gui, v *gocui.View) error {
			return scrollViewMsgs(g, -viewMsgsHeight/2)
		}); err != nil {
		log.Panicln(err)
	}
	if err := g.SetKeybinding("", gocui.KeyCtrlD, gocui.ModNone,
		func(g *gocui.Gui, v *gocui.View) error {
			return scrollViewMsgs(g, viewMsgsHeight/2)
		}); err != nil {
		log.Panicln(err)
	}
	// TODO
	// F11 / F12: scroll nicklist
	// F9 / F10: scroll roomlist
	// PgUp / PgDn: scroll text in current buffer

	// Initialize eventLoop channels
	sentMsgsChan = make(chan MessageRoom, 16)
	recvMsgsChan = make(chan MessageRoom, 16)
	rePrintChan = make(chan string, 16)
	switchRoomChan = make(chan bool, 16)
	cmdChan = make(chan []string, 16)

	go eventLoop(g)
	go cmdLoop(g)

	go func() {
		time.Sleep(time.Duration(30) * time.Second)
		rePrintChan <- "statusline"
	}()
	started = true
	if err := g.MainLoop(); err != nil && err != gocui.ErrQuit {
		return err
	}
	return nil
}

func strPadLeft(s string, pLen int, pad rune) string {
	if len(s) > pLen-2 {
		return s[:pLen-2] + ".."
	} else {
		return strings.Repeat(string(pad), pLen-len(s)) + s
	}
}

func strPadRight(s string, pLen int, pad rune) string {
	if len(s) > pLen-2 {
		return s[:pLen-2] + ".."
	} else {
		return s + strings.Repeat(string(pad), pLen-len(s))
	}
}
func setCurrentRoom(r *Room, toggle bool) {
	if toggle {
		if currentRoom == r {
			r = lastRoom
		}
	} else if currentRoom == r {
		return
	}
	lastRoom = currentRoom
	currentRoom = r
	if started {
		// TODO: Store readline buffer
		// TODO: Store viewMsgs origin (or bottom)
		switchRoomChan <- true
	}
}

func setViewMsgsHeight(g *gocui.Gui) {
	_, maxY := g.Size()
	viewMsgsHeight = maxY - 1 - readlineHeight
}

func layout(g *gocui.Gui) error {
	maxX, maxY := g.Size()
	winNewSize := false
	if windowWidth != maxX || windowHeight != maxY {
		windowWidth = maxX
		windowHeight = maxY
		setViewMsgsHeight(g)
		winNewSize = true
	}
	if winNewSize {
		if maxX < 2+viewRoomsWidth+timelineUserWidth+viewMsgsMinWidth+viewUsersWidth || maxY < 16 {
			v, err := g.SetView("err_win", -1, -1, maxX, maxY)
			if err != nil {
				if err != gocui.ErrUnknownView {
					return err
				}
			}
			fmt.Fprintln(v, "Window too small")
			g.SetViewOnTop("err_win")
			return nil
		} else {
			g.DeleteView("err_win")
			if err := setAllViews(g); err != nil {
				return err
			}
		}
	}
	if winNewSize {
		fmt.Fprintln(debugBuf, "New Size at", time.Now())
		viewMsgs, _ := g.View("msgs")
		printRoomMessages(viewMsgs, currentRoom)

	}
	return nil
}

func setAllViews(g *gocui.Gui) error {
	maxX, maxY := g.Size()
	if v, err := g.SetView("rooms", -1, -1, viewRoomsWidth, maxY); err != nil {
		if err != gocui.ErrUnknownView {
			return err
		}
		if err == gocui.ErrUnknownView {
			v.Frame = true
		}
		printRooms(v)
	}
	if v, err := g.SetView("users", maxX-viewUsersWidth, -1, maxX, maxY); err != nil {
		if err != gocui.ErrUnknownView {
			return err
		}
		if err == gocui.ErrUnknownView {
			v.Frame = true
		}
		printRoomUsers(v, currentRoom)
	}
	if v, err := g.SetView("readline", viewRoomsWidth, maxY-1-readlineHeight,
		maxX-viewUsersWidth, maxY); err != nil {
		if err != gocui.ErrUnknownView {
			return err
		}
		if err == gocui.ErrUnknownView {
			v.Frame = false
			v.Editable = true
			v.Editor = readLineEditor
		}
	}
	if v, err := g.SetView("statusline", viewRoomsWidth, maxY-2-readlineHeight,
		maxX-viewUsersWidth, maxY-readlineHeight); err != nil {
		if err != gocui.ErrUnknownView {
			return err
		}
		if err == gocui.ErrUnknownView {
			v.Frame = false
			v.BgColor = gocui.ColorBlue
		}
		printStatusLine(v, currentRoom)
	}
	viewMsgs, err := g.SetView("msgs", viewRoomsWidth, -1, maxX-viewUsersWidth, viewMsgsHeight)
	if err != nil {
		if err != gocui.ErrUnknownView {
			return err
		}
		if err == gocui.ErrUnknownView {
			viewMsgs.Frame = false
			viewMsgs.Wrap = true
		}
	}
	if _, err := g.View("debug"); err == nil {
		g.SetView("debug", maxX/2, maxY/2, maxX, maxY)
	}
	g.SetViewOnTop("statusline")
	g.SetCurrentView("readline")
	return nil
}

func printRooms(v *gocui.View) {
	v.Clear()
	pad := 1
	if len(rs.R) > 9 {
		pad = 2
	}
	roomSets := [][]*Room{[]*Room{rs.ConsoleRoom}, rs.PeopleRooms, rs.GroupRooms}
	for i, roomSet := range roomSets {
		switch i {
		case 1:
			fmt.Fprintf(v, "\n    People\n\n")
		case 2:
			fmt.Fprintf(v, "\n    Groups\n\n")
		}
		for _, r := range roomSet {
			highStart := ""
			highEnd := ""
			if r == currentRoom {
				highStart = "\x1b[48;5;40m\x1b[38;5;0m"
				highEnd = "\x1b[0;0m"
			}
			fmt.Fprintf(v, "%s%*s.%s%s\n", highStart, pad,
				fmt.Sprintf("%d", r.Shortcut),
				strPadRight(r.String(), viewRoomsWidth-pad, ' '),
				highEnd)
		}
	}

	//fmt.Fprintf(v, "%*s.%s\n", pad, fmt.Sprintf("%d", rs.ConsoleRoom.Shortcut), rs.ConsoleRoom)
	//fmt.Fprintf(v, "\n    People\n\n")
	//for _, r := range rs.PeopleRooms {
	//	fmt.Fprintf(v, "%*s.%s\n", pad, fmt.Sprintf("%d", r.Shortcut), r)
	//}

	//fmt.Fprintf(v, "\n    Groups\n\n")
	//for _, r := range rs.GroupRooms {
	//	fmt.Fprintf(v, "%*s.%s\n", pad, fmt.Sprintf("%d", r.Shortcut), r)
	//}
}

func printRoomMessages(v *gocui.View, r *Room) {
	v.Clear()
	viewMsgsLines = 0
	for e := r.Msgs.Front(); e != nil; e = e.Next() {
		m := e.Value.(Message)
		printMessage(v, &m, r)
	}
}

func printRoomUsers(v *gocui.View, r *Room) {
	v.Clear()
	for _, u := range r.Users.U {
		power := " "
		if u.Power > 50 {
			// Colored '@'
			power = "\x1b[38;5;220m@\x1b[0;0m"
		} else if u.Power > 0 {
			// Colored '+'
			power = "\x1b[38;5;172m+\x1b[0;0m"
		}
		//color := nick256Colors[u.DispNameHash%uint32(len(nick256Colors))]
		//username := fmt.Sprintf("\x1b[38;5;%dm%s\x1b[0;0m", color, u.DispName)
		//fmt.Fprintf(v, "%s%s\n", power, username)
		fmt.Fprintf(v, "%s%s\n", power, u)
	}
}

func printStatusLine(v *gocui.View, r *Room) {
	v.Clear()
	u := currentRoom.Users.ById[myUserId]
	power := ""
	if u.Power > 50 {
		power = "@"
	} else if u.Power > 0 {
		power = "+"
	}
	fmt.Fprintf(v, "\x1b[0;37m[%s] [%s%s] %d.%v [%d]",
		time.Now().Format("15:04"),
		power, myUsername,
		currentRoom.Shortcut, currentRoom, len(currentRoom.Users.U))
}

func printMessage(v *gocui.View, m *Message, r *Room) {
	msgWidth, _ := v.Size()
	t := time.Unix(m.Ts, 0)
	user := r.Users.ById[m.UserId]
	//displayName := ""
	//if user.Power >= 50 {
	//	displayName = fmt.Sprintf("@%s", user)
	//} else {
	//	displayName = fmt.Sprintf("%s", user)
	//}
	//fmt.Fprintln(debugBuf, r.Users.U[1])
	//fmt.Fprintln(debugBuf, r.Users.ById[user.Id])
	username := strPadLeft(user.String(), timelineUserWidth-10, ' ')
	//color := nickRGBColors[user.DispNameHash%uint32(len(nickRGBColors))]
	//username = fmt.Sprintf("\x1b[38;2;%d;%d;%dm%s\x1b[0;0m", color.r, color.g, color.b, username)
	color := nick256Colors[user.DispNameHash%uint32(len(nick256Colors))]
	username = fmt.Sprintf("\x1b[38;5;%dm%s\x1b[0;0m", color, username)
	//username = fmt.Sprintf("\x1b[38;2;255;0;0m%s\x1b[0;0m", "HOLA")
	fmt.Fprint(v, t.Format("15:04:05"), " ", username, " ")
	lines := 1
	strLen := 0
	for i, w := range strings.Split(m.Body, " ") {
		if strLen+len(w)+1 > msgWidth-timelineUserWidth {
			if strLen != 0 {
				fmt.Fprint(v, "\n", strings.Repeat(" ", timelineUserWidth))
				lines += 1
				strLen = 0
			}
			fmt.Fprint(v, w)
			lines += int(len(w) / msgWidth)
			strLen += len(w) % msgWidth
		} else {
			if i != 0 {
				fmt.Fprint(v, " ")
				strLen += 1
			}
			fmt.Fprint(v, w)
			strLen += len(w)
		}
	}
	fmt.Fprint(v, "\n")
	viewMsgsLines += lines
}

func quit(g *gocui.Gui) error {
	return gocui.ErrQuit
}

func keyDebugToggle(g *gocui.Gui, v *gocui.View) error {
	maxX, maxY := g.Size()
	if _, err := g.View("debug"); err == nil {
		g.DeleteView("debug")
		return nil
	}
	viewDebug, err := g.SetView("debug", maxX/2, maxY/2, maxX, maxY)
	if err != nil {
		if err != gocui.ErrUnknownView {
			return err
		}
		if err == gocui.ErrUnknownView {
			viewDebug.Frame = true
			viewDebug.Title = "Debug"
			viewDebug.Wrap = true
		}
		fmt.Fprint(viewDebug, debugBuf)
		g.SetViewOnTop("debug")
	}
	return nil
}

func sendMsg(body string, room *Room) error {
	msg := Message{"m.text", time.Now().Unix(), myUserId, body}
	sentMsgsChan <- MessageRoom{&msg, room}
	return nil
}

func keyReadmultiLineToggle(g *gocui.Gui, v *gocui.View) error {
	viewReadline, err := g.View("readline")
	if err != nil {
		return err
	}
	if readlineMultiline {
		readlineMultiline = false
		readlineHeight = 1
		setViewMsgsHeight(g)
		viewReadline.Editor = readLineEditor
	} else {
		readlineMultiline = true
		readlineHeight = 5
		setViewMsgsHeight(g)
		viewReadline.Editor = readMultiLineEditor
	}
	return nil
}
