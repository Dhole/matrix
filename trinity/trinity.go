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
	"unicode/utf8"
)

type RGBColor struct { // U
	r, g, b byte
}

type Message struct {
	MsgType string
	Ts      int64
	UserID  string
	Body    string
}

type Membership int // B

const ( // B
	MemInvite Membership = iota
	MemJoin   Membership = iota
	MemLeave  Membership = iota
)

const (
	ConsoleUserID      string = "@trinity:localhost"
	ConsoleDisplayName string = "trinity"
	ConsoleRoomID      string = "!console:localhost"
)

type User struct {
	ID           string     // B
	Name         string     // B
	DispName     string     // B
	DispNameHash uint32     // U
	Power        int        // B
	Mem          Membership // B
}

type Users struct {
	U      []*User            // B
	ByID   map[string]*User   // B
	ByName map[string][]*User // B
}

type Room struct {
	ID                  string     // B
	Name                string     // B
	DispName            string     // B
	CanonAlias          string     // B
	Topic               string     // B
	Shortcut            int        // U
	Fav                 bool       // U
	Users               Users      // B
	NewUserBatch        bool       // B
	Msgs                *list.List // B
	ViewMsgsOriginY     int        // U
	ScrollBottom        bool       // U
	ViewReadlineBuf     string     // U
	ViewReadlineCursorX int        // U
}

type Rooms struct {
	R           []*Room            // B
	ByID        map[string]*Room   // B
	ByName      map[string][]*Room // B
	ByShortcut  map[int]*Room      // U
	PeopleRooms []*Room            // U
	GroupRooms  []*Room            // U
	ConsoleRoom *Room              // B
}

type MessageRoom struct {
	Message *Message
	Room    *Room
}

type Args struct { // U
	Room *Room
	Args []string
}

type Words []string // U

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

var displayNamesID = false

// Callbacks
var callSendText func(roomID, body string)
var callJoinRoom func(roomIDorAlias string)
var callLeaveRoom func(roomID string)
var callQuit func()

// END CONFIG

// GLOBALS

var myDisplayName string
var myUserID string

var currentRoom *Room
var lastRoom *Room
var rs Rooms

var debugBuf *bytes.Buffer

var readLineEditor gocui.Editor = gocui.EditorFunc(readLine)
var readMultiLineEditor gocui.Editor = gocui.EditorFunc(readMultiLine)

var readlineMultiline bool
var readlineBuf []Words
var readlineIDx int

var viewMsgsHeight int
var viewMsgsLines int
var scrollBottom bool

// eventLoop channels
var sentMsgsChan chan MessageRoom
var recvMsgsChan chan MessageRoom
var rePrintChan chan string
var switchRoomChan chan bool
var cmdChan chan Args

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
	if r.CanonAlias != "" {
		r.DispName = r.CanonAlias
		return
	}
	roomUserIDs := make([]string, 0)
	for k := range r.Users.ByID {
		if k == myUserID {
			continue
		}
		roomUserIDs = append(roomUserIDs, k)
	}
	if len(roomUserIDs) == 1 {
		r.DispName = r.Users.ByID[roomUserIDs[0]].String()
		return
	}
	sort.Strings(roomUserIDs)
	if len(roomUserIDs) == 2 {
		r.DispName = fmt.Sprintf("%s and %s", r.Users.ByID[roomUserIDs[0]],
			r.Users.ByID[roomUserIDs[1]])
		return
	}
	if len(roomUserIDs) > 2 {
		r.DispName = fmt.Sprintf("%s and %d others", r.Users.ByID[roomUserIDs[0]],
			len(roomUserIDs)-1)
		return
	}
	r.DispName = "Emtpy room"
}

func (u *User) String() string {
	if displayNamesID {
		return u.ID
	} else {
		return u.DispName
	}
}

func (u *User) UpdateDispName(r *Room, roomUpdateDispName bool) {
	if roomUpdateDispName {
		defer r.UpdateDispName()
	}
	defer func() {
		u.DispNameHash = adler32.Checksum([]byte(u.DispName))
	}()
	if u.Name == "" {
		u.DispName = u.ID
		return
	}
	if len(r.Users.ByName[u.Name]) > 1 {
		u.DispName = fmt.Sprintf("%s (%s)", u.Name, u.ID)
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
	case key == gocui.KeyDelete:
		v.EditDelete(false)
	case key == gocui.KeyArrowLeft:
		v.MoveCursor(-1, 0, false)
	case key == gocui.KeyArrowRight:
		v.MoveCursor(1, 0, false)
		_, y := v.Origin()
		if y > 0 {
			v.MoveCursor(-1, 0, false)
		}
	case key == gocui.KeyCtrlA || key == gocui.KeyHome:
		v.SetOrigin(0, 0)
		v.SetCursor(0, 0)
	case key == gocui.KeyCtrlE || key == gocui.KeyEnd:
		v.MoveCursor(0, 1, false)
		v.MoveCursor(-1, 0, false)
	case key == gocui.KeyCtrlU:
		v.Clear()
		v.SetOrigin(0, 0)
		v.SetCursor(0, 0)
	case key == gocui.KeyEnter:
		body := v.Buffer()
		if len(body) == 0 {
			return
		}
		body = body[:len(body)-1]
		v.Clear()
		v.SetOrigin(0, 0)
		v.SetCursor(0, 0)
		if len(body) != 0 {
			sendMsg(body, currentRoom)
		}
	}
}

func readMultiLine(v *gocui.View, key gocui.Key, ch rune, mod gocui.Modifier) {
	// TODO
	readLine(v, key, ch, mod)
}

func NewUsers() (us Users) {
	us.U = make([]*User, 0)
	us.ByID = make(map[string]*User, 0)
	us.ByName = make(map[string][]*User, 0)
	return us
}

func (us *Users) Add(id, name string, power int, mem Membership) {
	u := &User{
		ID:           id,
		Name:         name,
		DispName:     "",
		DispNameHash: 0,
		Power:        power,
		Mem:          mem,
	}
	us.U = append(us.U, u)
	us.ByID[u.ID] = u
	if u.Name != "" {
		if us.ByName[u.Name] == nil {
			us.ByName[u.Name] = make([]*User, 0)
		}
		us.ByName[u.Name] = append(us.ByName[u.Name], u)
	}
	//fmt.Fprintf(debugBuf, "us:      %p\n", &us.U[len(us.U)-1])
	//fmt.Fprintf(debugBuf, "us.ByID: %p\n", us.ByID[u.ID])
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

func NewRoom(id, name, canonAlias, topic string) (r *Room) {
	r = &Room{}
	r.ID = id
	r.Name = name
	r.CanonAlias = canonAlias
	r.Topic = topic
	r.Users = NewUsers()
	//r.Msgs = make([]Message, 0)
	r.Msgs = list.New()
	return r
}

func NewRooms() (rs Rooms) {
	rs.R = make([]*Room, 0)
	rs.ByID = make(map[string]*Room, 0)
	rs.ByName = make(map[string][]*Room, 0)
	return rs
}

func (rs *Rooms) Add(r *Room) {
	rs.R = append(rs.R, r)
	rs.ByID[r.ID] = r
	if r.Name != "" {
		if rs.ByName[r.Name] == nil {
			rs.ByName[r.Name] = make([]*Room, 0)
		}
		rs.ByName[r.Name] = append(rs.ByName[r.Name], r)
	}
}

func (rs *Rooms) Del(r *Room) {
	delete(rs.ByID, r.ID)
	if r.Name != "" {
		delete(rs.ByName, r.Name)
	}
	newR := make([]*Room, 0, len(rs.R)-1)
	for _, room := range rs.R {
		if room == r {
			continue
		}
		newR = append(newR, room)
	}
	rs.R = newR
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

func SetMyDisplayName(username string) {
	myDisplayName = username
}

func SetMyUserID(userID string) {
	myUserID = userID
}

func SetCallSendText(call func(roomID, body string)) {
	callSendText = call
}

func SetCallJoinRoom(call func(roomIDorAlias string)) {
	callJoinRoom = call
}

func SetCallLeaveRoom(call func(roomID string)) {
	callLeaveRoom = call
}

func SetCallQuit(call func()) {
	callQuit = call
}

func AddRoom(roomID, name, canonAlias, topic string) error {
	_, ok := rs.ByID[roomID]
	if ok {
		return fmt.Errorf("Room %v already exists", roomID)
	}
	r := NewRoom(roomID, name, canonAlias, topic)
	r.UpdateDispName()
	rs.Add(r)
	rs.UpdateShortcuts()
	if started {
		rePrintChan <- "rooms"
	}
	return nil
}

func DelRoom(roomID string) (string, error) {
	r, ok := rs.ByID[roomID]
	if !ok {
		return "", fmt.Errorf("Room %v doesn't exists", roomID)
	}
	rs.Del(r)
	rs.UpdateShortcuts()
	if started {
		rePrintChan <- "rooms"
	}
	return r.Name, nil
}

func AddUser(roomID, userID, username string, power int, membership Membership) error {
	r, ok := rs.ByID[roomID]
	if !ok {
		return fmt.Errorf("Room %v doesn't exist", roomID)
	}
	r.Users.Add(userID, username, power, membership)
	for _, u := range r.Users.U {
		u.UpdateDispName(r, true)
	}
	r.UpdateDispName()
	if started {
		if r == currentRoom {
			rePrintChan <- "users"
			rePrintChan <- "statusline"
		}
		if len(r.Users.U) <= 3 {
			rs.UpdateShortcuts()
			rePrintChan <- "rooms"
		}
	}
	return nil
}

func AddUserBatch(roomID, userID, username string, power int, membership Membership) error {
	r, ok := rs.ByID[roomID]
	if !ok {
		return fmt.Errorf("Room %v doesn't exist", roomID)
	}
	r.Users.Add(userID, username, power, membership)
	r.NewUserBatch = true
	return nil
}

func AddUserBatchFinish() {
	updatedCurrentRoom := false
	for _, r := range rs.R {
		if !r.NewUserBatch {
			continue
		}
		for _, u := range r.Users.U {
			u.UpdateDispName(r, false)
		}
		r.UpdateDispName()
		r.NewUserBatch = false
		if r == currentRoom {
			updatedCurrentRoom = true
		}
	}
	if started {
		rs.UpdateShortcuts()
		rePrintChan <- "rooms"
		if updatedCurrentRoom {
			rePrintChan <- "users"
			rePrintChan <- "msgs"
			rePrintChan <- "statusline"
		}
	}
}

func AddConsoleMessage(body string) {
	AddMessage(ConsoleRoomID, "m.text", time.Now().Unix(), ConsoleUserID, body)
}

// DEBUG
func PushFrontMessage(roomID, msgType string, ts int64, userID, body string) error {
	r, ok := rs.ByID[roomID]
	if !ok {
		return fmt.Errorf("Room %v doesn't exist", roomID)
	}
	m := Message{msgType, ts, userID, body}
	// Deliberately doesn't reprint the room because this is DEBUG
	r.Msgs.PushFront(m)
	return nil
}

func AddMessage(roomID, msgType string, ts int64, userID, body string) error {
	r, ok := rs.ByID[roomID]
	if !ok {
		return fmt.Errorf("Room %v doesn't exist", roomID)
	}
	_, ok = r.Users.ByID[userID]
	if !ok {
		// We tolerate messages from non existing users.  We take care
		// in printMessage of that case
		AddConsoleMessage(fmt.Sprintf("AddMessage: User %v doesn't exist in room %v",
			userID, roomID))
	}
	m := Message{msgType, ts, userID, body}
	if started {
		recvMsgsChan <- MessageRoom{&m, r}
	} else {
		r.Msgs.PushBack(m)
	}
	return nil
}

func Debugln(args ...interface{}) {
	fmt.Fprintln(debugBuf, args...)
	rePrintChan <- "debug"
}

func Debugf(format string, args ...interface{}) {
	fmt.Fprintf(debugBuf, format, args...)
	fmt.Fprint(debugBuf, "\n")
	rePrintChan <- "debug"
}

func initReadline() {
	readlineBuf = make([]Words, 1)
	readlineIDx = 0
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
		case "debug":
			v, err := g.View(view)
			if err == nil {
				v.Clear()
				fmt.Fprint(v, debugBuf)
			}
		}
	}
}

func eventLoop(g *gocui.Gui) {
	for {
		select {
		case mr := <-sentMsgsChan:
			var r *Room
			if mr.Message.Body[0] == '/' {
				r = rs.ConsoleRoom
			} else {
				r = mr.Room
			}
			if r == rs.ConsoleRoom {
				appendRoomMsg(g, r, mr.Message)
				body := strings.TrimPrefix(mr.Message.Body, "/")
				args := strings.Fields(body)
				if len(args) < 1 {
					continue
				}
				cmdChan <- Args{mr.Room, args}
			} else {
				// TODO: Show the message temorarily until we
				// get echoed by the server
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

//func joinRoom(roomIDorAlias string) {
//	roomID, err := callJoinRoom(roomIDorAlias); err != nil {
//		AddConsoleMessage(fmt.Sprint("join:", err))
//	} else {
//		AddConsoleMessage(fmt.Sprintf("Joined room (%s) %s: %s",
//			roomID, resp["name"], resp["topic"]))
//	}
//}

//func leaveRoom(roomID string) {
//	r := rs.ByID[roomID]
//	if r == nil {
//		return
//	}
//	roomName := r.Name
//	if err := callLeaveRoom(roomID); err != nil {
//		AddConsoleMessage(fmt.Sprint("leave:", err))
//	} else {
//		AddConsoleMessage(fmt.Sprintf("Left room (%s) %s",
//			roomID, roomName))
//	}
//}

func roomIDCmd(args Args) string {
	if len(args.Args) == 2 {
		return args.Args[1]
	} else if len(args.Args) == 1 {
		return args.Room.ID
	} else {
		return ""
	}
}

func cmdLoop(g *gocui.Gui) {
	for {
		args := <-cmdChan
		switch args.Args[0] {
		case "quit":
			g.Execute(quit)
		case "join":
			if len(args.Args) != 2 {
				consoleReply(fmt.Sprintf("Usage: %s roomIDorAlias", args.Args[0]))
			} else {
				go callJoinRoom(args.Args[1])
			}
		case "leave":
			roomID := roomIDCmd(args)
			if roomID == "" {
				consoleReply(fmt.Sprintf("Usage: %s roomID", args.Args[0]))
				return
			}
			go callLeaveRoom(roomID)
			setCurrentRoom(lastRoom, false)
			lastRoom = currentRoom
		case "reset": // Clear gocui artifacts from the screen
			g.Execute(func(g *gocui.Gui) error {
				maxX, maxY := g.Size()
				v, err := g.SetView("clear", -1, -1, maxX, maxY)
				if err != nil {
					if err != gocui.ErrUnknownView {
						return err
					}
				}
				for i := 0; i < maxY; i++ {
					fmt.Fprintln(v, strings.Repeat("*", maxX))
				}
				g.SetViewOnTop("clear")
				return nil
			})
			time.Sleep(time.Duration(50) * time.Millisecond)
			g.Execute(func(g *gocui.Gui) error {
				g.DeleteView("clear")
				return nil
			})

		default:
			consoleReply(fmt.Sprintf("Unknown command: %s", args.Args[0]))
		}
	}
}

func consoleReply(rep string) {
	recvMsgsChan <- MessageRoom{
		&Message{"m.text", time.Now().Unix(), ConsoleUserID, rep},
		rs.ConsoleRoom}
}

func Init() {
	rs = NewRooms()
	AddRoom(ConsoleRoomID, "Console", "", "")
	AddUser(ConsoleRoomID, ConsoleUserID, ConsoleDisplayName, 100, MemJoin)
	rs.ConsoleRoom = rs.ByID[ConsoleRoomID]
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
	if myUserID == "" {
		return fmt.Errorf("UserID not set")
	}
	AddUser(ConsoleRoomID, myUserID, myDisplayName, 0, MemJoin)
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
	if err := g.SetKeybinding("", gocui.KeyPgup, gocui.ModNone,
		func(g *gocui.Gui, v *gocui.View) error {
			return scrollViewMsgs(g, -viewMsgsHeight/2)
		}); err != nil {
		log.Panicln(err)
	}
	if err := g.SetKeybinding("", gocui.KeyPgdn, gocui.ModNone,
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
	cmdChan = make(chan Args, 16)

	go eventLoop(g)
	go cmdLoop(g)

	go func() {
		time.Sleep(time.Duration(30) * time.Second)
		rePrintChan <- "statusline"
	}()
	rs.UpdateShortcuts()
	started = true
	if err := g.MainLoop(); err != nil && err != gocui.ErrQuit {
		return err
	}
	//callQuit()
	return nil
}

func strPadLeft(s string, pLen int, pad rune) string {
	sLen := utf8.RuneCountInString(s)
	if sLen > pLen-2 {
		return s[:pLen-2] + ".."
	} else {
		return strings.Repeat(string(pad), pLen-sLen) + s
	}
}

func strPadRight(s string, pLen int, pad rune) string {
	sLen := utf8.RuneCountInString(s)
	if sLen > pLen-2 {
		return s[:pLen-2] + ".."
	} else {
		return s + strings.Repeat(string(pad), pLen-sLen)
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
		//fmt.Fprintln(debugBuf, "New Size at", time.Now())
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
	u := currentRoom.Users.ByID[myUserID]
	power := ""
	if u != nil {
		if u.Power > 50 {
			power = "@"
		} else if u.Power > 0 {
			power = "+"
		}
	}
	fmt.Fprintf(v, "\x1b[0;37m[%s] [%s%s] %d.%v [%d] %s",
		time.Now().Format("15:04"),
		power, myDisplayName,
		currentRoom.Shortcut, currentRoom, len(currentRoom.Users.U),
		currentRoom.Topic)
}

func printMessage(v *gocui.View, m *Message, r *Room) {
	msgWidth, _ := v.Size()
	t := time.Unix(m.Ts, 0)
	user := r.Users.ByID[m.UserID]
	var color byte
	if user == nil {
		user = &User{ID: m.UserID, DispName: m.UserID, DispNameHash: 0}
		color = 244
	} else {
		color = nick256Colors[user.DispNameHash%uint32(len(nick256Colors))]
	}
	//displayName := ""
	//if user.Power >= 50 {
	//	displayName = fmt.Sprintf("@%s", user)
	//} else {
	//	displayName = fmt.Sprintf("%s", user)
	//}
	//fmt.Fprintln(debugBuf, r.Users.U[1])
	//fmt.Fprintln(debugBuf, r.Users.ByID[user.ID])
	username := strPadLeft(user.String(), timelineUserWidth-10, ' ')
	//color := nickRGBColors[user.DispNameHash%uint32(len(nickRGBColors))]
	//username = fmt.Sprintf("\x1b[38;2;%d;%d;%dm%s\x1b[0;0m", color.r, color.g, color.b, username)
	username = fmt.Sprintf("\x1b[38;5;%dm%s\x1b[0;0m", color, username)
	//username = fmt.Sprintf("\x1b[38;2;255;0;0m%s\x1b[0;0m", "HOLA")
	fmt.Fprint(v, t.Format("15:04:05"), " ", username, " ")
	timeLineSpace := strings.Repeat(" ", timelineUserWidth)
	viewMsgsWidth := msgWidth - timelineUserWidth
	body := strings.Replace(m.Body, "\x1b", "\\x1b", -1)
	lines := 0
	for i, l := range strings.Split(body, "\n") {
		strLen := 0
		if i != 0 {
			fmt.Fprint(v, timeLineSpace)
		}
		for j, w := range strings.Split(l, " ") {
			wLen := utf8.RuneCountInString(w)
			if !utf8.ValidString(w) {
				w = "\x1b[38;5;1mUTF8-ERR\x1b[0;0m"
				wLen = len("UTF8-ERR")
			}
			if strLen+wLen+1 > viewMsgsWidth {
				if strLen != 0 {
					fmt.Fprint(v, "\n", timeLineSpace)
					lines += 1
					strLen = 0
				}
				if wLen <= viewMsgsWidth {
					fmt.Fprint(v, w)
					strLen += wLen
					continue
				}
				wRunes := bytes.Runes([]byte(w))
				for len(wRunes) != 0 {
					end := min(viewMsgsWidth, len(wRunes))
					chunk := wRunes[:end]
					wRunes = wRunes[end:]
					fmt.Fprint(v, string(chunk))
					if len(wRunes) != 0 {
						fmt.Fprint(v, "\n", timeLineSpace)
						lines += 1
					}
				}
				strLen += wLen % viewMsgsWidth
			} else {
				if j != 0 {
					fmt.Fprint(v, " ")
					strLen += 1
				}
				fmt.Fprint(v, w)
				strLen += wLen
			}
		}
		fmt.Fprint(v, "\n")
		lines += 1
	}
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
	if body[0] == '/' || room == rs.ConsoleRoom {
		msg := Message{"m.text", time.Now().Unix(), myUserID, body}
		sentMsgsChan <- MessageRoom{&msg, room}
	} else {
		go callSendText(room.ID, body)
	}
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
