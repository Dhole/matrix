package morpheus

import (
	"../list"
	"fmt"
	"sort"
	"time"
)

const (
	ConsoleUserID      string = "@morpheus:localhost"
	ConsoleDisplayName string = "morpheus"
	ConsoleRoomID      string = "!console:localhost"
)

type Message struct {
	MsgType string
	ID      string
	Ts      int64
	UserID  string
	Content interface{}
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
	UI       interface{}
}

func (u *User) String() string {
	return u.DispName
}

// If myUserID != "", update the room display name
func (u *User) updateDispName(r *Room, myUserID string) {
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
	ByID map[string]*User
	// TODO: Concurrent write and/or read is not ok
	ByName map[string][]*User
	Room   *Room
}

func newUsers(r *Room) (us Users) {
	us.U = make([]*User, 0)
	us.ByID = make(map[string]*User, 0)
	us.ByName = make(map[string][]*User, 0)
	us.Room = r
	return us
}

func (us *Users) Add(id, name string, power int, mem Membership) (*User, error) {
	u, err := us.AddBatch(id, name, power, mem)
	if err != nil {
		return nil, err
	}
	us.Room.Rooms.call.AddUser(us.Room, u)
	for _, u := range us.U {
		u.updateDispName(us.Room, *us.Room.myUserID)
	}
	us.Room.updateDispName(*us.Room.myUserID)
	return u, nil
}

func (us *Users) AddBatch(id, name string, power int, mem Membership) (*User, error) {
	if us.ByID[id] != nil {
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
	us.ByID[u.ID] = u
	if u.Name != "" {
		if us.ByName[u.Name] == nil {
			us.ByName[u.Name] = make([]*User, 0)
		}
		us.ByName[u.Name] = append(us.ByName[u.Name], u)
	}
	return u, nil
}

func (us *Users) AddBatchFinish() {
	for _, u := range us.U {
		us.Room.Rooms.call.AddUser(us.Room, u)
		u.updateDispName(us.Room, "")
	}
	us.Room.updateDispName(*us.Room.myUserID)
}

func (us *Users) Del(u *User) {
	// TODO
	// TODO: What if there are two users with the same name?
	us.Room.Rooms.call.DelUser(us.Room, u)
}

func (us *Users) SetUserName(u *User, name string) {
	// TODO
	// TODO: What if there are two users with the same name?
	us.Room.Rooms.call.UpdateUser(us.Room, u)
}

type Room struct {
	ID         string
	Name       string
	DispName   string
	CanonAlias string
	Topic      string
	Users      Users
	Msgs       *list.List
	// TODO: Add mutex to manipulate Msgs
	myUserID *string

	Rooms *Rooms
	UI    interface{}
}

func NewRoom(rs *Rooms, id, name, canonAlias, topic string) (r *Room) {
	r = &Room{}
	r.ID = id
	r.Name = name
	r.CanonAlias = canonAlias
	r.Topic = topic
	r.Users = newUsers(r)
	//r.Msgs = make([]Message, 0)
	r.Msgs = list.New()
	r.Rooms = rs
	return r
}

func (r *Room) String() string {
	return r.DispName
}

func (r *Room) updateDispName(myUserID string) {
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

func (r *Room) AddMessage(msgType, id string, ts int64, userID string,
	content map[string]interface{}) error {

	cnt, err := parseMessage(msgType, content)
	if err != nil {
		return err
	}
	// TODO: Convert content into a struct.  Have convert as map[string]interface{}
	m := &Message{msgType, id, ts, userID, cnt}
	r.Msgs.PushBack(m)
	r.Rooms.call.ArrvMessage(r, m)
	return nil
}

func (r *Room) AddTextMessage(id string, ts int64, userID, body string) error {
	m := &Message{"m.text", id, ts, userID, TextMessage{body}}
	r.Msgs.PushBack(m)
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
	r.Msgs.PushFront(m)
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

	call Callbacks
	UI   interface{}
}

func NewRooms(call Callbacks) (rs Rooms) {
	rs.R = make([]*Room, 0)
	rs.ByID = make(map[string]*Room, 0)
	rs.ByName = make(map[string][]*Room, 0)
	rs.call = call
	return rs
}

func (rs *Rooms) Add(myUserID *string, roomID, name, canonAlias, topic string) (*Room, error) {
	_, ok := rs.ByID[roomID]
	if ok {
		return nil, fmt.Errorf("Room %v already exists", roomID)
	}
	r := NewRoom(rs, roomID, name, canonAlias, topic)
	r.myUserID = myUserID
	r.updateDispName(*r.myUserID)
	rs.R = append(rs.R, r)
	rs.ByID[r.ID] = r
	if r.Name != "" {
		if rs.ByName[r.Name] == nil {
			rs.ByName[r.Name] = make([]*Room, 0)
		}
		rs.ByName[r.Name] = append(rs.ByName[r.Name], r)
	}
	rs.call.AddRoom(r)
	return r, nil
}

func (rs *Rooms) Del(roomID string) (*Room, error) {
	r, ok := rs.ByID[roomID]
	if !ok {
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
	rs.call.DelRoom(r)
	return r, nil
}

func (rs *Rooms) SetRoomName(r *Room, name string) {
	// TODO
	// TODO: What if there are two rooms with the same name?
	rs.call.UpdateRoom(r)
}

func (rs *Rooms) AddConsoleMessage(msgType string, content map[string]interface{}) error {
	return rs.ConsoleRoom.AddMessage(msgType, "1", time.Now().Unix()*1000,
		rs.ConsoleUserID, content)
}

func (rs *Rooms) AddConsoleTextMessage(body string) error {
	return rs.ConsoleRoom.AddTextMessage("1", time.Now().Unix()*1000,
		rs.ConsoleUserID, body)
}
