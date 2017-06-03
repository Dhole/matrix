package morpheus

import (
	"../list"
	"fmt"
	"github.com/matrix-org/gomatrix"
	"sort"
	"strconv"
	"sync"
	//sync "github.com/sasha-s/go-deadlock"
	"time"
)

const (
	ConsoleUserID      string = "@morpheus:localhost"
	ConsoleDisplayName string = "morpheus"
	ConsoleRoomID      string = "!console:localhost"
)

type Token string

func txnID() string {
	return "go" + strconv.FormatInt(time.Now().UnixNano(), 10)
}

type Message struct {
	MsgType string
	//ID      string
	//Ts      int64
	//Sender  string
	Content interface{}
}

type Event struct {
	Type     string
	ID       string
	Ts       int64
	Sender   string
	StateKey *string
	Content  interface{}
}

type Events struct {
	l   *list.List
	rwm *sync.RWMutex
}

func NewEvents() (evs Events) {
	evs.l = list.New()
	evs.rwm = &sync.RWMutex{}
	return evs
}

func (evs *Events) PushBack(e interface{}) {
	evs.rwm.Lock()
	evs.l.PushBack(e)
	evs.rwm.Unlock()
}

func (evs *Events) PushFront(e interface{}) {
	evs.rwm.Lock()
	evs.l.PushFront(e)
	evs.rwm.Unlock()
}

func (evs *Events) Front() *list.Element {
	evs.rwm.RLock()
	defer evs.rwm.RUnlock()
	return evs.l.Front()
}

func (evs *Events) Back() *list.Element {
	evs.rwm.RLock()
	defer evs.rwm.RUnlock()
	return evs.l.Back()
}

func (evs *Events) LastEvent() *Event {
	evs.rwm.RLock()
	defer evs.rwm.RUnlock()
	for e := evs.l.Back(); e != nil; e = e.Prev() {
		// do something with e.Value
		if ev, ok := e.Value.(*Event); ok {
			return ev
		}
	}
	return nil
}

func (evs *Events) Len() int {
	evs.rwm.RLock()
	defer evs.rwm.RUnlock()
	return evs.l.Len()
}

func (evs *Events) Iterator() *EventsIterator {
	evs.rwm.RLock()
	return &EventsIterator{cur: evs.l.Front(), rwm: evs.rwm}
}

type EventsIterator struct {
	cur *list.Element
	rwm *sync.RWMutex
}

func (evsIt *EventsIterator) Next() *list.Element {
	defer func() {
		if evsIt.cur == nil {
			evsIt.rwm.RUnlock()
		} else {
			evsIt.cur = evsIt.cur.Next()
		}
	}()
	return evsIt.cur
}

type MsgTxtType int

const (
	MsgTxtTypeText   MsgTxtType = iota
	MsgTxtTypeEmote  MsgTxtType = iota
	MsgTxtTypeNotice MsgTxtType = iota
)

type TextMessage struct {
	Body string
	Type MsgTxtType
}

type StateRoomName struct {
	Name string
}

type StateRoomCanonAlias struct {
	Alias string
}

type StateRoomTopic struct {
	Topic string
}

type StateRoomJoinRules struct {
	IsPublic bool
}

type StateRoomMember struct {
	Name       string
	Membership Membership
}

type Membership int

const (
	MemInvite     Membership = iota
	MemJoin       Membership = iota
	MemLeave      Membership = iota
	MemBan        Membership = iota
	MembershipLen Membership = iota
)

func (mem Membership) String() string {
	switch mem {
	case MemInvite:
		return "MemInvite"
	case MemJoin:
		return "MemJoin"
	case MemLeave:
		return "MemLeave"
	case MemBan:
		return "MemBan"
	default:
		return ""
	}
}

type RoomState int

const (
	RoomStateAll        RoomState = iota
	RoomStateName       RoomState = iota
	RoomStateDispName   RoomState = iota
	RoomStateTopic      RoomState = iota
	RoomStateMembership RoomState = iota
)

type User struct {
	ID       string
	Name     string
	DispName string
	Power    int
	Mem      Membership

	rwm sync.RWMutex
	UI  interface{}
}

func (u *User) String() string {
	return u.DispName
}

// If myUserID != "", update the room display name
func (u *User) updateDispName(r *Room) {
	u.rwm.Lock()
	defer u.rwm.Unlock()
	prevDispName := u.DispName
	//if myUserID != "" {
	//	defer r.updateDispName(myUserID)
	//}
	defer func() {
		if u.DispName != prevDispName {
			r.Rooms.call.UpdateUser(r, u)
		}
	}()
	if u.Name == "" {
		u.DispName = u.ID
		return
	}
	if r.Users.byNameCount[u.Name] > 1 {
		u.DispName = fmt.Sprintf("%s (%s)", u.Name, u.ID)
		return
	}
	u.DispName = u.Name
	return
}

func (u *User) setName(name string, r *Room) {
	u.rwm.Lock()
	if u.Name == name {
		u.rwm.Unlock()
		return
	} else if u.Name != "" {
		r.Users.byNameCount[u.Name]--
	}
	u.Name = name
	u.rwm.Unlock()
	u.updateDispName(r)
	if u.Name != "" {
		r.Users.byNameCount[u.Name]++
	}
}

func (u *User) setPower(power int, r *Room) {
	u.rwm.Lock()
	if u.Power == power {
		u.rwm.Unlock()
		return
	}
	u.Power = power
	u.rwm.Unlock()
}

func (u *User) setMembership(mem Membership, newUser bool, r *Room) {
	u.rwm.Lock()
	if u.Mem == mem {
		u.rwm.Unlock()
		return
	}
	u.Mem = mem
	if newUser {
		r.Users.MemCountDelta(u.Mem, mem, 0, 1)
	} else {
		r.Users.MemCountDelta(u.Mem, mem, -1, 1)
	}
	u.rwm.Unlock()
}

type Users struct {
	U []*User
	// TODO: Concurrent write and/or read is not ok
	byID map[string]*User
	// TODO: Concurrent write and/or read is not ok
	byNameCount map[string]uint
	// TODO: Add byDispName map[string]*User

	MemCount [MembershipLen]int

	Room *Room
	rwm  *sync.RWMutex
}

func (us *Users) ByID(id string) *User {
	us.rwm.RLock()
	defer us.rwm.RUnlock()
	return us.byID[id]
}

func (us *Users) MemCountDelta(memOld, memNew Membership, deltaOld, deltaNew int) {
	us.rwm.Lock()
	us.MemCount[memOld] += deltaOld
	us.MemCount[memNew] += deltaNew
	us.rwm.Unlock()
}

func newUsers(r *Room) (us Users) {
	us.U = make([]*User, 0)
	us.byID = make(map[string]*User, 0)
	us.byNameCount = make(map[string]uint, 0)
	us.Room = r
	us.rwm = &sync.RWMutex{}
	return us
}

// Add or Update the User
func (us *Users) Add(id, name string, power int, mem Membership) (*User, error) {
	us.rwm.Lock()

	updateDispName := false
	newUser := false
	u := us.byID[id]
	if u == nil {
		updateDispName = true
		newUser = true
		u = &User{ID: id}
		us.U = append(us.U, u)
		us.byID[u.ID] = u
		us.Room.Rooms.call.AddUser(us.Room, u)
	} else if u.Name != name {
		updateDispName = true
	}
	us.rwm.Unlock()

	u.setName(name, us.Room)
	u.setPower(power, us.Room)
	u.setMembership(mem, newUser, us.Room)

	//return u, fmt.Errorf("User %v already exists in this room", id)

	us.rwm.RLock()
	if updateDispName {
		for _, u := range us.U {
			u.updateDispName(us.Room)
		}
	}
	us.rwm.RUnlock()

	if updateDispName {
		us.Room.updateDispName(*us.Room.myUserID)
	}
	us.Room.Rooms.call.UpdateUser(us.Room, u)
	return u, nil
}

//func (us *Users) addBatch(id, name string, power int, mem Membership) (*User, error) {
//	if u := us.byID[id]; u != nil {
//		u.setName(name)
//		u.setPower(power)
//		u.setMembership(mem)
//		return u, fmt.Errorf("User %v already exists in this room", id)
//	}
//	u := &User{
//		ID:       id,
//		Name:     name,
//		DispName: "",
//		Power:    power,
//		Mem:      mem,
//	}
//	us.U = append(us.U, u)
//	us.byID[u.ID] = u
//	return u, nil
//}

//func (us *Users) AddBatch(id, name string, power int, mem Membership) (*User, error) {
//	us.rwm.Lock()
//	defer us.rwm.Unlock()
//	u, err := us.addBatch(id, name, power, mem)
//	return u, err
//}
//
//func (us *Users) AddBatchFinish() {
//	us.rwm.RLock()
//	for _, u := range us.U {
//		us.Room.Rooms.call.AddUser(us.Room, u)
//		u.updateDispName(us.Room)
//	}
//	us.rwm.RUnlock()
//	us.Room.updateDispName(*us.Room.myUserID)
//}

func (us *Users) Del(u *User) {
	us.rwm.Lock()
	// TODO
	// TODO: What if there are two users with the same name?
	us.rwm.Unlock()
	us.Room.Rooms.call.DelUser(us.Room, u)
}

func (us *Users) SetUserName(u *User, name string) {
	us.rwm.Lock()
	// TODO
	// TODO: What if there are two users with the same name?
	us.rwm.Unlock()
	us.Room.Rooms.call.UpdateUser(us.Room, u)
}

type ExpBackoff struct {
	t   uint32
	max uint32
}

func NewExpBackoff(max uint32) ExpBackoff {
	return ExpBackoff{0, max}
}

func (eb *ExpBackoff) Inc() {
	if eb.t == 0 {
		eb.t = 200
		return
	}
	if eb.t < eb.max {
		eb.t *= 2
	}
}

func (eb *ExpBackoff) Wait() {
	if eb.t != 0 {
		time.Sleep(time.Duration(eb.t) * time.Millisecond)
	}
}

func (eb *ExpBackoff) Reset() {
	eb.t = 0
}

type Room struct {
	ID         string
	Name       string
	DispName   string
	CanonAlias string
	Topic      string
	Users      Users
	//Msgs        *list.List
	Events Events
	//msgsLen     int
	tokensLen   int
	HasFirstMsg bool
	HasLastMsg  bool
	myUserID    *string
	Mem         Membership

	Rooms      *Rooms
	rwm        sync.RWMutex
	ExpBackoff ExpBackoff
	UI         interface{}
}

func NewRoom(rs *Rooms, id string, mem Membership, name, canonAlias, topic string) (r *Room) {
	r = &Room{}
	r.ID = id
	r.Mem = mem
	r.Name = name
	r.CanonAlias = canonAlias
	r.Topic = topic
	r.Users = newUsers(r)
	r.Events = NewEvents()
	r.Rooms = rs
	r.ExpBackoff = NewExpBackoff(30000)
	return r
}

//func (r *Room) MsgsLen() int {
//	return r.msgsLen
//}

func (r *Room) String() string {
	return r.DispName
}

func (r *Room) updateDispName(myUserID string) {
	r.rwm.Lock()
	defer r.rwm.Unlock()
	prevDispName := r.DispName
	defer func() {
		if r.DispName != prevDispName {
			r.Rooms.call.UpdateRoom(r, RoomStateDispName)
		}
	}()
	if r.Name != "" {
		r.DispName = r.Name
		return
	}
	if r.CanonAlias != "" {
		r.DispName = r.CanonAlias
		return
	}
	roomUserIDs := make([]string, 0)
	for _, u := range r.Users.U {
		if u.ID == myUserID {
			continue
		}
		roomUserIDs = append(roomUserIDs, u.ID)
	}
	sort.Strings(roomUserIDs)
	if len(roomUserIDs) == 1 {
		r.DispName = r.Users.ByID(roomUserIDs[0]).String()
		return
	}
	if len(roomUserIDs) == 2 {
		r.DispName = fmt.Sprintf("%s and %s", r.Users.ByID(roomUserIDs[0]),
			r.Users.ByID(roomUserIDs[1]))
		return
	}
	if len(roomUserIDs) > 2 {
		r.DispName = fmt.Sprintf("%s and %d others", r.Users.ByID(roomUserIDs[0]),
			len(roomUserIDs)-1)
		return
	}
	r.DispName = "Emtpy room"
}

func parseMessage(msgType string, content map[string]interface{}) (interface{}, error) {
	var cnt interface{}
	var msgTxtType MsgTxtType
	isMsgTxt := false
	switch msgType {
	case "m.text":
		msgTxtType = MsgTxtTypeText
		isMsgTxt = true
	case "m.emote":
		msgTxtType = MsgTxtTypeEmote
		isMsgTxt = true
	case "m.notice":
		msgTxtType = MsgTxtTypeNotice
		isMsgTxt = true
	default:
		return nil, fmt.Errorf("msgtype %s not supported yet", msgType)
	}
	if isMsgTxt {
		var mc TextMessage
		body, ok := content["body"].(string)
		if !ok {
			return nil, fmt.Errorf("Error decoding msgtype %s with content %+v",
				msgType, content)
		}
		mc.Body = body
		mc.Type = msgTxtType
		cnt = mc
	}
	return cnt, nil
}

func parseEvent(evType string, stateKey *string,
	content map[string]interface{}) (interface{}, error) {
	var cnt interface{}
	switch evType {
	//case "m.room.aliases":
	case "m.room.canonical_alias":
		alias, ok := content["alias"].(string)
		if !ok {
			return nil, fmt.Errorf("Error decoding event %s with content %+v",
				evType, content)
		}
		cnt = StateRoomCanonAlias{Alias: alias}
	//case "m.room.create":
	case "m.room.join_rules":
		joinRule, ok := content["join_rule"].(string)
		if !ok {
			return nil, fmt.Errorf("Error decoding event %s with content %+v",
				evType, content)
		}
		switch joinRule {
		case "public":
			cnt = StateRoomJoinRules{IsPublic: true}
		case "invite":
			cnt = StateRoomJoinRules{IsPublic: false}
		default:
			return nil, fmt.Errorf("Unhandled join_rule %s", joinRule)
		}
	case "m.room.member":
		mem, ok := content["membership"].(string)
		if !ok {
			return nil, fmt.Errorf("Error decoding event %s with content %+v",
				evType, content)
		}
		name, ok := content["displayname"].(string)
		if !ok {
			name = ""
		}
		membership := MemJoin
		switch mem {
		case "invite":
			membership = MemInvite
		case "join":
			membership = MemJoin
		case "leave":
			membership = MemLeave
		case "ban":
			membership = MemBan
		default:
			return nil, fmt.Errorf("Error decoding event %s with content %+v",
				evType, content)
		}
		cnt = StateRoomMember{Name: name, Membership: membership}
	//case "m.room.power_levels":
	//case "m.room.redaction":
	case "m.room.message": // Stateless
		msgType, ok := content["msgtype"].(string)
		if !ok {
			return nil, fmt.Errorf("Invalid %s", evType)
		}
		mc, err := parseMessage(msgType, content)
		if err != nil {
			return nil, err
		}
		cnt = Message{msgType, mc}
	//case "m.room.message.feedback": // Stateless
	case "m.room.name":
		name, ok := content["name"].(string)
		if !ok {
			return nil, fmt.Errorf("Error decoding event %s with content %+v",
				evType, content)
		}
		cnt = StateRoomName{Name: name}
	case "m.room.topic":
		topic, ok := content["topic"].(string)
		if !ok {
			return nil, fmt.Errorf("Error decoding event %s with content %+v",
				evType, content)
		}
		cnt = StateRoomTopic{Topic: topic}
	//case "m.room.avatar":
	default:
		return nil, fmt.Errorf("event %s not supported yet", evType)
	}
	return cnt, nil
}

func (r *Room) SetName(name string) {
	r.rwm.Lock()
	r.Name = name
	r.rwm.Unlock()
	r.updateDispName(*r.myUserID)
}

func (r *Room) SetCanonAlias(alias string) {
	r.rwm.Lock()
	r.CanonAlias = alias
	r.rwm.Unlock()
	r.updateDispName(*r.myUserID)
}

func (r *Room) SetTopic(topic string) {
	r.rwm.Lock()
	r.Topic = topic
	r.rwm.Unlock()
	r.Rooms.call.UpdateRoom(r, RoomStateTopic)
}

func (r *Room) SetMembership(mem Membership) {
	r.rwm.Lock()
	r.Mem = mem
	r.rwm.Unlock()
	r.Rooms.call.UpdateRoom(r, RoomStateMembership)
}

func (r *Room) PushToken(token string) {
	r.Events.PushBack(Token(token))
	r.tokensLen++
}

func (r *Room) PushFrontToken(token string) {
	r.Events.PushFront(Token(token))
	r.tokensLen++
}

func (r *Room) PushMessage(msgType, id string, ts int64, userID string,
	content map[string]interface{}) error {

	cnt, err := parseMessage(msgType, content)
	if err != nil {
		return err
	}
	e := &Event{"m.room.message", id, ts, userID, nil, Message{msgType, cnt}}
	r.Events.PushBack(e)
	//r.msgsLen++
	r.Rooms.call.ArrvMessage(r, e)
	return nil
}

func (r *Room) PushTextMessage(txtType MsgTxtType, id string, ts int64, userID, body string) error {
	e := &Event{"m.room.message", id, ts, userID, nil,
		Message{"m.text", TextMessage{body, txtType}}}
	r.Events.PushBack(e)
	//r.msgsLen++
	r.Rooms.call.ArrvMessage(r, e)
	return nil
}

func (r *Room) PushEvent(ev *gomatrix.Event) error {
	cnt, err := parseEvent(ev.Type, ev.StateKey, ev.Content)
	if err != nil {
		return err
	}
	e := &Event{ev.Type, ev.ID, int64(ev.Timestamp), ev.Sender, ev.StateKey, cnt}
	r.Events.PushBack(e)
	//r.msgsLen++
	r.Rooms.call.ArrvMessage(r, e)
	return nil
}

func (r *Room) PushFrontMessage(msgType, id string, ts int64, userID string,
	content map[string]interface{}) error {

	cnt, err := parseMessage(msgType, content)
	if err != nil {
		return err
	}
	e := &Event{"m.room.message", id, ts, userID, nil, Message{msgType, cnt}}
	r.Events.PushFront(e)
	//r.msgsLen++
	return nil
}

func (r *Room) updateState(ev *gomatrix.Event) error {
	cnt, err := parseEvent(ev.Type, ev.StateKey, ev.Content)
	if err != nil {
		return err
	}
	switch cnt := cnt.(type) {
	case StateRoomName:
		r.SetName(cnt.Name)
	case StateRoomTopic:
		r.SetTopic(cnt.Topic)
	case StateRoomCanonAlias:
		r.SetCanonAlias(cnt.Alias)
	case StateRoomMember:
		if ev.StateKey == nil || *ev.StateKey == "" {
			return fmt.Errorf("m.room.member doesn't have a state key")
		}
		r.Users.Add(*ev.StateKey, cnt.Name, 0, cnt.Membership)
		if cnt.Membership == MemLeave && ev.StateKey == r.myUserID {
			r.SetMembership(MemLeave)
		}
	default:
		return fmt.Errorf("Event type not handled yet")
	}
	return nil
}

type Callbacks struct {
	AddUser    func(r *Room, u *User)
	DelUser    func(r *Room, u *User)
	UpdateUser func(r *Room, u *User)

	AddRoom    func(r *Room)
	DelRoom    func(r *Room)
	UpdateRoom func(r *Room, state RoomState)

	ArrvMessage func(r *Room, e *Event)

	Cmd func(r *Room, args []string)
}

type Rooms struct {
	R []*Room
	// TODO: Concurrent write and/or read is not ok
	byID map[string]*Room
	// TODO: Concurrent write and/or read is not ok
	byName             map[string][]*Room
	ConsoleRoom        *Room
	consoleRoomID      string
	ConsoleDisplayName string
	ConsoleUserID      string

	rwm  *sync.RWMutex
	call Callbacks
	UI   interface{}
}

func (rs *Rooms) ByID(id string) *Room {
	rs.rwm.RLock()
	defer rs.rwm.RUnlock()
	return rs.byID[id]
}

func (rs *Rooms) ByName(name string) []*Room {
	rs.rwm.RLock()
	defer rs.rwm.RUnlock()
	return rs.byName[name]
}

func NewRooms(call Callbacks) (rs Rooms) {
	rs.R = make([]*Room, 0)
	rs.byID = make(map[string]*Room, 0)
	rs.byName = make(map[string][]*Room, 0)
	rs.rwm = &sync.RWMutex{}
	rs.call = call
	return rs
}

// Add or Update the Room
func (rs *Rooms) Add(myUserID *string, roomID string, mem Membership) (*Room, error) {
	rs.rwm.Lock()
	r, ok := rs.byID[roomID]
	if ok {
		r.SetMembership(mem)
		rs.rwm.Unlock()
		return r, fmt.Errorf("Room %v already exists", roomID)
	}
	r = NewRoom(rs, roomID, mem, "", "", "")
	r.myUserID = myUserID
	rs.R = append(rs.R, r)
	rs.byID[r.ID] = r
	if r.Name != "" {
		rs.byName[r.Name] = append(rs.byName[r.Name], r)
	}
	rs.rwm.Unlock()

	rs.call.AddRoom(r)
	r.updateDispName(*r.myUserID)
	rs.call.UpdateRoom(r, RoomStateAll)
	return r, nil
}

func (rs *Rooms) Del(roomID string) (*Room, error) {
	rs.rwm.Lock()
	r, ok := rs.byID[roomID]
	if !ok {
		rs.rwm.Unlock()
		return nil, fmt.Errorf("Room %v doesn't exists", roomID)
	}
	delete(rs.byID, r.ID)
	if r.Name != "" {
		delete(rs.byName, r.Name)
	}
	newR := make([]*Room, 0, len(rs.R)-1)
	for _, room := range rs.R {
		if room == r {
			continue
		}
		newR = append(newR, room)
	}
	rs.R = newR
	rs.rwm.Unlock()
	rs.call.DelRoom(r)
	return r, nil
}

func (rs *Rooms) SetRoomName(r *Room, name string) {
	rs.rwm.Lock()
	// TODO
	// TODO: What if there are two rooms with the same name?
	rs.rwm.Unlock()
	rs.call.UpdateRoom(r, RoomStateName)
}

func (rs *Rooms) AddConsoleMessage(msgType string, content map[string]interface{}) error {
	return rs.ConsoleRoom.PushMessage(msgType, txnID(), time.Now().Unix()*1000,
		rs.ConsoleUserID, content)
}

func (rs *Rooms) AddConsoleTextMessage(txtType MsgTxtType, body string) error {
	return rs.ConsoleRoom.PushTextMessage(txtType, txnID(), time.Now().Unix()*1000,
		rs.ConsoleUserID, body)
}
