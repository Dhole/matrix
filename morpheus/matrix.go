package morpheus

import (
	"../list"
	"fmt"
	"sort"
	"strconv"
	"sync"
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
	ID      string
	Ts      int64
	UserID  string
	Content interface{}
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

type TextMessage struct {
	Body string
}

type Membership int

const (
	MemInvite Membership = iota
	MemJoin   Membership = iota
	MemLeave  Membership = iota
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
func (u *User) updateDispName(r *Room, myUserID string) {
	u.rwm.Lock()
	defer u.rwm.Unlock()
	prevDispName := u.DispName
	defer func() {
		if u.DispName != prevDispName {
			r.Rooms.call.UpdateUser(r, u)
		}
	}()
	if myUserID != "" {
		defer r.updateDispName(myUserID)
	}
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

type Users struct {
	U []*User
	// TODO: Concurrent write and/or read is not ok
	byID map[string]*User
	// TODO: Concurrent write and/or read is not ok
	ByName map[string][]*User

	Room *Room
	rwm  *sync.RWMutex
}

func (us *Users) ByID(id string) *User {
	us.rwm.RLock()
	defer us.rwm.RUnlock()
	return us.byID[id]
}

func newUsers(r *Room) (us Users) {
	us.U = make([]*User, 0)
	us.byID = make(map[string]*User, 0)
	us.ByName = make(map[string][]*User, 0)
	us.Room = r
	us.rwm = &sync.RWMutex{}
	return us
}

func (us *Users) Add(id, name string, power int, mem Membership) (*User, error) {
	us.rwm.Lock()
	u, err := us.addBatch(id, name, power, mem)
	if err != nil {
		us.rwm.Unlock()
		return nil, err
	}
	us.Room.Rooms.call.AddUser(us.Room, u)
	for _, u := range us.U {
		u.updateDispName(us.Room, *us.Room.myUserID)
	}
	us.rwm.Unlock()

	us.Room.updateDispName(*us.Room.myUserID)
	us.Room.Rooms.call.UpdateUser(us.Room, u)
	return u, nil
}

func (us *Users) addBatch(id, name string, power int, mem Membership) (*User, error) {
	if us.byID[id] != nil {
		return nil, fmt.Errorf("User %v already exists in this room", id)
	}
	u := &User{
		ID:       id,
		Name:     name,
		DispName: "",
		Power:    power,
		Mem:      mem,
	}
	us.U = append(us.U, u)
	us.byID[u.ID] = u
	if u.Name != "" {
		if us.ByName[u.Name] == nil {
			us.ByName[u.Name] = make([]*User, 0)
		}
		us.ByName[u.Name] = append(us.ByName[u.Name], u)
	}
	return u, nil
}

func (us *Users) AddBatch(id, name string, power int, mem Membership) (*User, error) {
	us.rwm.Lock()
	defer us.rwm.Unlock()
	u, err := us.addBatch(id, name, power, mem)
	return u, err
}

func (us *Users) AddBatchFinish() {
	us.rwm.RLock()
	for _, u := range us.U {
		us.Room.Rooms.call.AddUser(us.Room, u)
		u.updateDispName(us.Room, "")
	}
	us.Room.updateDispName(*us.Room.myUserID)
	us.rwm.RUnlock()
}

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
	// TODO: Add mutex to manipulate Msgs
	myUserID *string

	Rooms *Rooms
	rwm   sync.RWMutex
	UI    interface{}
}

func NewRoom(rs *Rooms, id, name, canonAlias, topic string) (r *Room) {
	r = &Room{}
	r.ID = id
	r.Name = name
	r.CanonAlias = canonAlias
	r.Topic = topic
	r.Users = newUsers(r)
	r.Events = NewEvents()
	r.Rooms = rs
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
			r.Rooms.call.UpdateRoom(r)
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
	switch msgType {
	case "m.text":
		var cm TextMessage
		body, ok := content["body"].(string)
		if !ok {
			return nil, fmt.Errorf("Error decoding msgtype %s with content %+v",
				msgType, content)
		}
		cm.Body = body
		cnt = cm
	default:
		return nil, fmt.Errorf("msgtype %s not supported yet", msgType)
	}
	return cnt, nil
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
	// TODO: Convert content into a struct.  Have convert as map[string]interface{}
	m := &Message{msgType, id, ts, userID, cnt}
	r.Events.PushBack(m)
	//r.msgsLen++
	r.Rooms.call.ArrvMessage(r, m)
	return nil
}

func (r *Room) PushTextMessage(id string, ts int64, userID, body string) error {
	m := &Message{"m.text", id, ts, userID, TextMessage{body}}
	r.Events.PushBack(m)
	//r.msgsLen++
	r.Rooms.call.ArrvMessage(r, m)
	return nil
}

func (r *Room) PushFrontMessage(msgType, id string, ts int64, userID string,
	content map[string]interface{}) error {

	cnt, err := parseMessage(msgType, content)
	if err != nil {
		return err
	}
	m := &Message{msgType, id, ts, userID, cnt}
	r.Events.PushFront(m)
	//r.msgsLen++
	return nil
}

type Callbacks struct {
	AddUser    func(r *Room, u *User)
	DelUser    func(r *Room, u *User)
	UpdateUser func(r *Room, u *User)

	AddRoom    func(r *Room)
	DelRoom    func(r *Room)
	UpdateRoom func(r *Room)

	ArrvMessage func(r *Room, m *Message)

	Cmd func(r *Room, args []string)
}

type Rooms struct {
	R []*Room
	// TODO: Concurrent write and/or read is not ok
	ByID map[string]*Room
	// TODO: Concurrent write and/or read is not ok
	ByName             map[string][]*Room
	ConsoleRoom        *Room
	consoleRoomID      string
	ConsoleDisplayName string
	ConsoleUserID      string

	rwm  *sync.RWMutex
	call Callbacks
	UI   interface{}
}

func NewRooms(call Callbacks) (rs Rooms) {
	rs.R = make([]*Room, 0)
	rs.ByID = make(map[string]*Room, 0)
	rs.ByName = make(map[string][]*Room, 0)
	rs.rwm = &sync.RWMutex{}
	rs.call = call
	return rs
}

func (rs *Rooms) Add(myUserID *string, roomID, name, canonAlias, topic string) (*Room, error) {
	rs.rwm.Lock()
	_, ok := rs.ByID[roomID]
	if ok {
		rs.rwm.Unlock()
		return nil, fmt.Errorf("Room %v already exists", roomID)
	}
	r := NewRoom(rs, roomID, name, canonAlias, topic)
	r.myUserID = myUserID
	rs.R = append(rs.R, r)
	rs.ByID[r.ID] = r
	if r.Name != "" {
		if rs.ByName[r.Name] == nil {
			rs.ByName[r.Name] = make([]*Room, 0)
		}
		rs.ByName[r.Name] = append(rs.ByName[r.Name], r)
	}
	rs.rwm.Unlock()

	rs.call.AddRoom(r)
	r.updateDispName(*r.myUserID)
	rs.call.UpdateRoom(r)
	return r, nil
}

func (rs *Rooms) Del(roomID string) (*Room, error) {
	rs.rwm.Lock()
	r, ok := rs.ByID[roomID]
	if !ok {
		rs.rwm.Unlock()
		return nil, fmt.Errorf("Room %v doesn't exists", roomID)
	}
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
	rs.rwm.Unlock()
	rs.call.DelRoom(r)
	return r, nil
}

func (rs *Rooms) SetRoomName(r *Room, name string) {
	rs.rwm.Lock()
	// TODO
	// TODO: What if there are two rooms with the same name?
	rs.rwm.Unlock()
	rs.call.UpdateRoom(r)
}

func (rs *Rooms) AddConsoleMessage(msgType string, content map[string]interface{}) error {
	return rs.ConsoleRoom.PushMessage(msgType, txnID(), time.Now().Unix()*1000,
		rs.ConsoleUserID, content)
}

func (rs *Rooms) AddConsoleTextMessage(body string) error {
	return rs.ConsoleRoom.PushTextMessage(txnID(), time.Now().Unix()*1000,
		rs.ConsoleUserID, body)
}
