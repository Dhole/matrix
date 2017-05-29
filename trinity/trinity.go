package main

import (
	mor "../morpheus"
	"bytes"
	"fmt"
	"sync"
	//"github.com/jroimartin/gocui"
	"../../gocui"
	"hash/adler32"
	//"io"
	"log"
	"strings"
	"time"
	"unicode/utf8"
)

// FIXME: Console room doesn't scroll to bottom

type RGBColor struct {
	r, g, b byte
}

type Words []string

type RoomEvent struct {
	Room  *mor.Room
	Event *mor.Event
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
	Shortcut        int
	Fav             bool
	ViewMsgsOriginY int
	ScrollBottom    bool
	ScrollSkipMsgs  uint
	//ScrollDelta         int
	gettingPrev         bool
	gettingPrevM        sync.Mutex
	ViewReadlineBuf     string
	ViewReadlineCursorX int
}

func (rUI *RoomUI) TryGettingPrev() bool {
	defer rUI.gettingPrevM.Unlock()
	rUI.gettingPrevM.Lock()
	if rUI.gettingPrev == false {
		rUI.gettingPrev = true
		return true
	} else {
		return false
	}
}

func (rUI *RoomUI) UnsetGettingPrev() {
	defer rUI.gettingPrevM.Unlock()
	rUI.gettingPrevM.Lock()
	rUI.gettingPrev = false
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

// END CONFIG

// GLOBALS

var cli *mor.Client

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
var recvMsgChan chan RoomEvent
var rePrintChan chan string
var scrollChan chan int
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
		case mod == gocui.ModAlt && ch >= '0' && ch <= '9':
			roomShortcut = int(ch - '0')
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

// DEBUG
func initReadline() {
	readlineBuf = make([]Words, 1)
	readlineIDx = 0
	readlineMultiline = false
}

func getPrevEvents(room *mor.Room) {
	roomUI := getRoomUI(room)
	defer func() {
		//roomUI.GettingPrev = false
		roomUI.UnsetGettingPrev()
	}()
	// Fetch previous messages
	count, err := cli.GetPrevEvents(currentRoom, uint(viewMsgsHeight*2))
	if err != nil || count == 0 {
		if currentRoom == room {
			scrollChan <- 0
		}
	}
	roomUI.ScrollSkipMsgs += count
	if currentRoom == room {
		rePrintChan <- "msgs"
		// At least load enough messages to cover the entire screen
		if !room.HasFirstMsg && (viewMsgsLines+1 < viewMsgsHeight) {
			// Force a new getPrevEvents due to scrolling past the top.
			scrollChan <- -1
			scrollChan <- +1
		}
	}
}

func scrollViewMsgs(viewMsgs *gocui.View, l int) error {
	_, y := viewMsgs.Origin()
	newY := 0
	if l <= 0 {
		newY = y + l
		if newY < 1 {
			newY = 0
			room := currentRoom
			roomUI := getRoomUI(room)
			if currentRoom.HasFirstMsg {
				newY = 1
			} else if roomUI.TryGettingPrev() {
				go getPrevEvents(room)
			}
		}
	} else {
		newY = min(y+l, viewMsgsLines-viewMsgsHeight)
		newY = max(newY, 1)
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
	viewMsgs, err := g.View("msgs")
	if err != nil {
		return err
	}
	return scrollViewMsgs(viewMsgs, viewMsgsLines-viewMsgsHeight)
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

// Events that trigger UI changes
func eventLoop(g *gocui.Gui) {
	for {
		select {
		case re := <-recvMsgChan:
			g.Execute(func(g *gocui.Gui) error {
				viewMsgs, _ := g.View("msgs")
				if re.Room == currentRoom {
					printMessage(viewMsgs, re.Event, re.Room)
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
		case l := <-scrollChan:
			g.Execute(func(g *gocui.Gui) error {
				viewMsgs, _ := g.View("msgs")
				scrollViewMsgs(viewMsgs, l)
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

func AddedUser(r *mor.Room, u *mor.User) {
	initUserUI(u)
	//UpdatedUser(r, u)
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
	roomUI := getRoomUI(r)
	roomUI.ViewMsgsOriginY = 1
	roomUI.ScrollBottom = true
	//rUI := getRoomUI(r)
	//UpdatedRoom(r)
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

func ArrvdMessage(r *mor.Room, e *mor.Event) {
	if started {
		if currentRoom == r {
			recvMsgChan <- RoomEvent{r, e}
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

	//
	// Start
	initReadline()
	g, err := gocui.NewGui(gocui.Output256)
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
			viewMsgs, err := g.View("msgs")
			if err != nil {
				return err
			}
			return scrollViewMsgs(viewMsgs, -1)
		}); err != nil {
		log.Panicln(err)
	}
	if err := g.SetKeybinding("", gocui.KeyArrowDown, gocui.ModNone,
		func(g *gocui.Gui, v *gocui.View) error {
			viewMsgs, err := g.View("msgs")
			if err != nil {
				return err
			}
			return scrollViewMsgs(viewMsgs, 1)
		}); err != nil {
		log.Panicln(err)
	}
	if err := g.SetKeybinding("", gocui.KeyPgup, gocui.ModNone,
		func(g *gocui.Gui, v *gocui.View) error {
			viewMsgs, err := g.View("msgs")
			if err != nil {
				return err
			}
			return scrollViewMsgs(viewMsgs, -viewMsgsHeight/2)
		}); err != nil {
		log.Panicln(err)
	}
	if err := g.SetKeybinding("", gocui.KeyPgdn, gocui.ModNone,
		func(g *gocui.Gui, v *gocui.View) error {
			viewMsgs, err := g.View("msgs")
			if err != nil {
				return err
			}
			return scrollViewMsgs(viewMsgs, viewMsgsHeight/2)
		}); err != nil {
		log.Panicln(err)
	}
	// TODO
	// F11 / F12: scroll nicklist
	// F9 / F10: scroll roomlist

	// Initialize eventLoop channels
	//sentMsgsChan = make(chan RoomMessage, 16)
	recvMsgChan = make(chan RoomEvent, 16)
	rePrintChan = make(chan string, 16)
	scrollChan = make(chan int, 16)
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

	err = <-exit
	if err != nil {
		panic(err)
	}
}

func strPadLeft(s string, pLen int, pad rune) string {
	sLen := RuneCountInStringNoEscape(s)
	if sLen > pLen-2 {
		// TODO: Make this utf-8
		return s[:pLen-2] + ".."
	} else {
		return strings.Repeat(string(pad), pLen-sLen) + s
	}
}

func strPadRight(s string, pLen int, pad rune) string {
	sLen := RuneCountInStringNoEscape(s)
	if sLen > pLen-2 {
		// TODO: Make this utf-8
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
		cli.SetMinMsgs(uint(viewMsgsHeight * 2))
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
			// DEBUG
			//viewMsgs.Wrap = true
			viewMsgs.SetOrigin(0, 1)
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
		if len(roomSet) != 0 {
			switch i {
			case 1:
				fmt.Fprintf(v, "\n    People\n\n")
			case 2:
				fmt.Fprintf(v, "\n    Groups\n\n")
			}
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
	viewMsgsWidth, _ := v.Size()
	prevLine := "--- Fetching previous messages ---"
	fmt.Fprintln(v, strings.Repeat(" ", viewMsgsWidth/2-len(prevLine)/2),
		"\x1b[38;5;45m", prevLine, "\x1b[0;0m")
	viewMsgsLines = 1
	roomUI := getRoomUI(r)
	count := uint(0)
	scrollDelta := 0
	prevTs := time.Unix(0, 0)
	prevMsgsBar := false
	it := r.Events.Iterator()
	for elem := it.Next(); elem != nil; elem = it.Next() {
		// DEBUG
		//if t, ok := elem.Value.(mor.Token); ok {
		//	fmt.Fprintf(v, "%s%s%s\n", "\x1b[38;5;226m-- Token: ", t, " --\x1b[0;0m")
		//	viewMsgsLines++
		//}
		if e, ok := elem.Value.(*mor.Event); ok {
			ts := time.Unix(e.Ts/1000, 0)
			if prevTs.Day() != ts.Day() ||
				prevTs.Month() != ts.Month() ||
				prevTs.Year() != ts.Year() {
				date := ts.Format("-- Mon, 02 Jan 2006 --")
				fmt.Fprintf(v, "%s%s%s\n", "\x1b[38;5;72m", date, "\x1b[0;0m")
				viewMsgsLines++
			} else if prevMsgsBar {
				// There was a date at the beginning of the
				// buffer before, but it's not there after the
				// previous messages bar
				scrollDelta--
			}
			if prevMsgsBar {
				prevMsgsBar = false
			}
			prevTs = ts
			printMessage(v, e, r)
			count++
			if count == roomUI.ScrollSkipMsgs {
				fmt.Fprintf(v, "%s%s%s\n", "\x1b[38;5;29m",
					strings.Repeat("–", viewMsgsWidth), "\x1b[0;0m")
				scrollDelta = viewMsgsLines
				viewMsgsLines++
				prevMsgsBar = true
			}
		}
	}
	if roomUI.ScrollSkipMsgs != 0 {
		//cli.ConsolePrint("roomUI.ScrollDelta = ", roomUI.ScrollDelta)
		scrollViewMsgs(v, scrollDelta) //+roomUI.ScrollDelta)
		roomUI.ScrollSkipMsgs = 0
		//roomUI.ScrollDelta = 0
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
	u := currentRoom.Users.ByID(cli.GetUserID())
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

func eventToStrings(e *mor.Event, r *mor.Room) (string, string, bool) {
	u := r.Users.ByID(e.Sender)
	var color byte
	var uUI *UserUI
	if u == nil {
		u = &mor.User{ID: e.Sender, DispName: e.Sender, UI: &UserUI{DispNameHash: 0}}
		uUI = getUserUI(u)
		color = 244
	} else {
		uUI = getUserUI(u)
		color = nick256Colors[uUI.DispNameHash%uint32(len(nick256Colors))]
	}

	text := ""
	username := ""
	switch ec := e.Content.(type) {
	case mor.Message:
		username = strPadLeft(u.String(), timelineUserWidth-10, ' ')
		username = fmt.Sprintf("\x1b[38;5;%dm%s\x1b[0;0m", color, username)
		switch mc := ec.Content.(type) {
		case mor.TextMessage:
			text = mc.Body
			text = strings.Replace(text, "\x1b", "\\x1b", -1)
		default:
			text = fmt.Sprintf("msgtype %s not supported yet", ec.MsgType)
		}
	default:
		text = fmt.Sprintf("DEBUG:%+v", e)
		username = strings.Repeat(" ", timelineUserWidth-10)
		return "", "", false
	}

	return username, text, true
}

func printMessage(v *gocui.View, e *mor.Event, r *mor.Room) {
	msgWidth, _ := v.Size()
	t := time.Unix(e.Ts/1000, 0)
	username, text, ok := eventToStrings(e, r)
	if !ok {
		return
	}
	fmt.Fprint(v, "\x1b[38;5;110m", t.Format("15:04:05"), "\x1b[0;0m", " ",
		username, " ")

	timeLineSpace := strings.Repeat(" ", timelineUserWidth)
	viewMsgsWidth := msgWidth - timelineUserWidth
	lines := 0
	for i, l := range strings.Split(text, "\n") {
		strLen := 0
		if i != 0 {
			fmt.Fprint(v, timeLineSpace)
		}
		for j, w := range strings.Split(l, " ") {
			wLen := RuneCountInStringNoEscape(w)
			if !utf8.ValidString(w) {
				w = "\x1b[38;5;1m�\x1b[0;0m"
				wLen = 1
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
	if text == "" {
		fmt.Fprint(v, "\n")
	}
	viewMsgsLines += lines
}

func quit(g *gocui.Gui) error {
	// DEBUG
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
