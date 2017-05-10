package main

import (
	mor "../morpheus"
	"bytes"
	"fmt"
	//"github.com/jroimartin/gocui"
	"../../gocui"
	"hash/adler32"
	//"io"
	"log"
	"strings"
	"time"
	"unicode/utf8"
)

type RGBColor struct {
	r, g, b byte
}

type Words []string

type RoomMessage struct {
	Room    *mor.Room
	Message *mor.Message
}

type Args struct {
	Room *mor.Room
	Args []string
}

type UserUI struct {
	DispNameHash uint32
}

func initUserUI(u *mor.User) {
	u.UI = &UserUI{}
}

func getUserUI(u *mor.User) *UserUI {
	return u.UI.(*UserUI)
}

type RoomUI struct {
	Shortcut            int
	Fav                 bool
	ViewMsgsOriginY     int
	ScrollBottom        bool
	ViewReadlineBuf     string
	ViewReadlineCursorX int
}

func initRoomUI(r *mor.Room) {
	r.UI = &RoomUI{}
}

func getRoomUI(r *mor.Room) *RoomUI {
	return r.UI.(*RoomUI)
}

type RoomsUI struct {
	ByShortcut  map[int]*mor.Room
	PeopleRooms []*mor.Room
	GroupRooms  []*mor.Room
}

func initRoomsUI(rs *mor.Rooms) {
	rs.UI = &RoomsUI{ByShortcut: make(map[int]*mor.Room)}
}

func getRoomsUI(rs *mor.Rooms) *RoomsUI {
	return rs.UI.(*RoomsUI)
}

func UpdateShortcuts(rs *mor.Rooms) {
	rsUI := getRoomsUI(rs)
	rsUI.ByShortcut = make(map[int]*mor.Room)
	rsUI.PeopleRooms = make([]*mor.Room, 0)
	rsUI.GroupRooms = make([]*mor.Room, 0)
	count := 0
	rsUI.ByShortcut[0] = rs.ConsoleRoom
	for _, r := range rs.R[1:] {
		if len(r.Users.U) == 2 {
			count++
			rUI := getRoomUI(r)
			rUI.Shortcut = count
			rsUI.PeopleRooms = append(rsUI.PeopleRooms, r)
			rsUI.ByShortcut[rUI.Shortcut] = r
		}
	}
	for _, r := range rs.R[1:] {
		if len(r.Users.U) != 2 {
			count++
			rUI := getRoomUI(r)
			rUI.Shortcut = count
			rsUI.GroupRooms = append(rsUI.GroupRooms, r)
			rsUI.ByShortcut[rUI.Shortcut] = r
		}
	}
}

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
//var callSendText func(roomID, body string)
//var callJoinRoom func(roomIDorAlias string)
//var callLeaveRoom func(roomID string)
//var callQuit func()

// END CONFIG

// GLOBALS

var cli *mor.Client

//var myDisplayName string
//var myUserID string

var currentRoom *mor.Room
var lastRoom *mor.Room

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
var recvMsgChan chan RoomMessage
var rePrintChan chan string
var switchRoomChan chan bool
var cmdChan chan Args

var started bool

// END GLOBALS

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
	rs := cli.Rs
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
		rsUI := getRoomsUI(&rs)
		r, ok := rsUI.ByShortcut[roomShortcut]
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
			sendText(body, currentRoom)
		}
	}
}

func readMultiLine(v *gocui.View, key gocui.Key, ch rune, mod gocui.Modifier) {
	// TODO
	readLine(v, key, ch, mod)
}

//func AddUser(roomID, userID, username string, power int, membership Membership) error {
//	if started {
//		if r == currentRoom {
//			rePrintChan <- "users"
//			rePrintChan <- "statusline"
//		}
//		if len(r.Users.U) <= 3 {
//			rs.UpdateShortcuts()
//			rePrintChan <- "rooms"
//		}
//	}
//}

// DEBUG
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

//func appendRoomMsg(g *gocui.Gui, r *Room, m *Message) {
//	r.Msgs.PushBack(*m)
//	g.Execute(func(g *gocui.Gui) error {
//		viewMsgs, _ := g.View("msgs")
//		if r == currentRoom {
//			printMessage(viewMsgs, m, r)
//			if scrollBottom {
//				scrollViewMsgsBottom(g)
//			}
//		}
//		return nil
//	})
//}

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

// Events that trigger UI changes
func eventLoop(g *gocui.Gui) {
	for {
		select {
		//case mr := <-sentMsgsChan:
		//	var r *Room
		//	if mr.Message.Body[0] == '/' {
		//		r = rs.ConsoleRoom
		//	} else {
		//		r = mr.Room
		//	}
		//	if r == rs.ConsoleRoom {
		//		appendRoomMsg(g, r, mr.Message)
		//		body := strings.TrimPrefix(mr.Message.Body, "/")
		//		args := strings.Fields(body)
		//		if len(args) < 1 {
		//			continue
		//		}
		//		cmdChan <- Args{mr.Room, args}
		//	} else {
		//		// TODO: Show the message temorarily until we
		//		// get echoed by the server
		//	}
		case rm := <-recvMsgChan:
			//appendRoomMsg(g, rm.Room, rm.Message)
			g.Execute(func(g *gocui.Gui) error {
				viewMsgs, _ := g.View("msgs")
				if rm.Room == currentRoom {
					printMessage(viewMsgs, rm.Message, rm.Room)
					if scrollBottom {
						scrollViewMsgsBottom(g)
					}
				}
				return nil
			})
		case view := <-rePrintChan:
			g.Execute(func(g *gocui.Gui) error {
				printView(g, view)
				return nil
			})
		case <-switchRoomChan:
			g.Execute(func(g *gocui.Gui) error {
				lastRoomUI := getRoomUI(lastRoom)
				currentRoomUI := getRoomUI(currentRoom)
				viewMsgs, _ := g.View("msgs")
				_, y := viewMsgs.Origin()
				lastRoomUI.ViewMsgsOriginY = y
				viewMsgs.SetOrigin(0, currentRoomUI.ViewMsgsOriginY)

				lastRoomUI.ScrollBottom = scrollBottom
				scrollBottom = currentRoomUI.ScrollBottom

				viewReadline, _ := g.View("readline")
				x, _ := viewReadline.Cursor()
				lastRoomUI.ViewReadlineBuf = viewReadline.Buffer()
				lastRoomUI.ViewReadlineCursorX = x
				viewReadline.Clear()
				viewReadline.SetOrigin(0, 0)
				viewReadline.Write([]byte(currentRoomUI.ViewReadlineBuf))
				viewReadline.SetCursor(currentRoomUI.ViewReadlineCursorX, 0)

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
				cli.ConsolePrintf("Usage: %s roomIDorAlias", args.Args[0])
			} else {
				go cli.JoinRoom(args.Args[1])
			}
		case "leave":
			roomID := roomIDCmd(args)
			if roomID == "" {
				cli.ConsolePrintf("Usage: %s roomID", args.Args[0])
				return
			}
			go cli.LeaveRoom(roomID)
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
			cli.ConsolePrintf("Unknown command: %s", args.Args[0])
		}
	}
}

//func consoleReply(rep string) {
//	RecvMsgsChan <- RoomMessage{
//		&Message{"m.text", time.Now().Unix() * 1000, ConsoleUserID, rep},
//		rs.ConsoleRoom}
//}

func AddedUser(r *mor.Room, u *mor.User) {
	initUserUI(u)
	UpdatedUser(r, u)
}

func DeletedUser(r *mor.Room, u *mor.User) {
	if started {
		UpdateShortcuts(&cli.Rs)
		rePrintChan <- "rooms"
		if currentRoom == r {
			rePrintChan <- "users"
			rePrintChan <- "statusline"
		}
	}
}

func UpdatedUser(r *mor.Room, u *mor.User) {
	uUI := getUserUI(u)
	uUI.DispNameHash = adler32.Checksum([]byte(u.DispName))
	if started {
		if currentRoom == r {
			rePrintChan <- "users"
			rePrintChan <- "statusline"
		}
		if len(r.Users.U) <= 3 {
			UpdateShortcuts(&cli.Rs)
			rePrintChan <- "rooms"
		}
	}
}

func AddedRoom(r *mor.Room) {
	initRoomUI(r)
	//rUI := getRoomUI(r)
	UpdatedRoom(r)
	if started {
		UpdateShortcuts(&cli.Rs)
	}
}

func DeletedRoom(r *mor.Room) {

	if started {
		UpdateShortcuts(&cli.Rs)
		if currentRoom == r {
			if lastRoom != r {
				currentRoom = lastRoom
			} else {
				currentRoom = cli.Rs.ConsoleRoom
			}
			rePrintChan <- "all"
		} else {
			rePrintChan <- "rooms"
		}
	}
}

func UpdatedRoom(r *mor.Room) {
	//rUI := getRoomUI(r)
	if started {
		rePrintChan <- "rooms"
		if currentRoom == r {
			rePrintChan <- "statusline"
		}
	}
}

func ArrvdMessage(r *mor.Room, m *mor.Message) {
	if started {
		if currentRoom == r {
			recvMsgChan <- RoomMessage{r, m}
		}
	}
}

func Cmd(r *mor.Room, args []string) {
	if started {
		cmdChan <- Args{r, args}
	}
}

func main() {
	var err error
	cli, err = mor.NewClient("morpheus", []string{"."}, mor.Callbacks{
		AddedUser, DeletedUser, UpdatedUser,
		AddedRoom, DeletedRoom, UpdatedRoom,
		ArrvdMessage,
		Cmd,
	})
	if err != nil {
		panic(err)
	}

	//
	// Init

	initRoomsUI(&cli.Rs)

	currentRoom = cli.Rs.ConsoleRoom
	lastRoom = currentRoom
	//currentRoom = rs.ConsoleRoom

	//
	// Start

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
	//sentMsgsChan = make(chan RoomMessage, 16)
	recvMsgChan = make(chan RoomMessage, 16)
	rePrintChan = make(chan string, 16)
	switchRoomChan = make(chan bool, 16)
	cmdChan = make(chan Args, 16)

	go eventLoop(g)
	go cmdLoop(g)

	UpdateShortcuts(&cli.Rs)
	exit := make(chan error)
	go func() {
		if err := g.MainLoop(); err != nil && err != gocui.ErrQuit {
			exit <- err
		} else {
			exit <- nil
		}
	}()

	go func() {
		time.Sleep(time.Duration(30) * time.Second)
		rePrintChan <- "statusline"
	}()

	// TODO: Error checking
	cli.Login()
	// TODO: Error checking
	go cli.Sync()
	//cli.ConsolePrint("Hello world!")
	//cli.ConsolePrintf("%+v", *cli.Rs.ConsoleRoom.Users.U[0])

	err = <-exit
	if err != nil {
		panic(err)
	}
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
func setCurrentRoom(r *mor.Room, toggle bool) {
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
		if maxX < 2+viewRoomsWidth+timelineUserWidth+viewMsgsMinWidth+viewUsersWidth ||
			maxY < 16 {
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
		cli.SetMinMsgs(uint(maxY * 3))
	}
	if !started {
		started = true
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
	rs := &cli.Rs
	v.Clear()
	pad := 1
	if len(rs.R) > 9 {
		pad = 2
	}
	rsUI := getRoomsUI(rs)
	roomSets := [][]*mor.Room{[]*mor.Room{rs.ConsoleRoom}, rsUI.PeopleRooms, rsUI.GroupRooms}
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
			rUI := getRoomUI(r)
			fmt.Fprintf(v, "%s%*s.%s%s\n", highStart, pad,
				fmt.Sprintf("%d", rUI.Shortcut),
				strPadRight(r.String(), viewRoomsWidth-pad, ' '),
				highEnd)
		}
	}
}

func printRoomMessages(v *gocui.View, r *mor.Room) {
	v.Clear()
	viewMsgsLines = 0
	for e := r.Msgs.Front(); e != nil; e = e.Next() {
		m := e.Value.(*mor.Message)
		printMessage(v, m, r)
	}
}

func printRoomUsers(v *gocui.View, r *mor.Room) {
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

func printStatusLine(v *gocui.View, r *mor.Room) {
	v.Clear()
	u := currentRoom.Users.ByID[cli.GetUserID()]
	power := "!"
	if u != nil {
		if u.Power > 50 {
			power = "@"
		} else if u.Power > 0 {
			power = "+"
		} else {
			power = ""
		}
	}
	fmt.Fprintf(v, "\x1b[0;37m[%s] [%s%s] %d.%v [%d] %s",
		time.Now().Format("15:04"),
		power, cli.GetDisplayName(),
		getRoomUI(currentRoom).Shortcut, currentRoom, len(currentRoom.Users.U),
		currentRoom.Topic)
}

func printMessage(v *gocui.View, m *mor.Message, r *mor.Room) {
	msgWidth, _ := v.Size()
	t := time.Unix(m.Ts/1000, 0)
	u := r.Users.ByID[m.UserID]
	var color byte
	var uUI *UserUI
	if u == nil {
		if r.ID == mor.ConsoleRoomID {
			cli.ConsolePrintf("%+v", r.Users.U)
		}
		u = &mor.User{ID: m.UserID, DispName: m.UserID, UI: &UserUI{DispNameHash: 0}}
		uUI = getUserUI(u)
		color = 244
	} else {
		uUI = getUserUI(u)
		color = nick256Colors[uUI.DispNameHash%uint32(len(nick256Colors))]
	}
	username := strPadLeft(u.String(), timelineUserWidth-10, ' ')
	//color := nickRGBColors[uUI.DispNameHash%uint32(len(nickRGBColors))]
	//username = fmt.Sprintf("\x1b[38;2;%d;%d;%dm%s\x1b[0;0m", color.r, color.g, color.b, username)
	username = fmt.Sprintf("\x1b[38;5;%dm%s\x1b[0;0m", color, username)
	//username = fmt.Sprintf("\x1b[38;2;255;0;0m%s\x1b[0;0m", "HOLA")
	fmt.Fprint(v, t.Format("15:04:05"), " ", username, " ")
	timeLineSpace := strings.Repeat(" ", timelineUserWidth)
	viewMsgsWidth := msgWidth - timelineUserWidth
	text := ""
	switch m.MsgType {
	case "m.text":
		text = m.Content.(mor.TextMessage).Body
	default:
		text = fmt.Sprintf("msgtype %s not supported yet", m.MsgType)
	}
	text = strings.Replace(text, "\x1b", "\\x1b", -1)
	lines := 0
	for i, l := range strings.Split(text, "\n") {
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
	// DEBUG
	// panicing for now because it hangs
	//panic("quit")
	go cli.StopSync()
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

func sendText(body string, r *mor.Room) error {
	go cli.SendText(r.ID, body)
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
