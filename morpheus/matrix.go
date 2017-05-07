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

type User struct {
	ID       string     // B
	Name     string     // B
	DispName string     // B
	Power    int        // B
	Mem      Membership // B
	UI       interface{}
}

func (u *User) String() string {
	return u.DispName
}

// If myUserID != "", update the room display name
func (u *User) UpdateDispName(r *Room, myUserID string) {
	if myUserID != "" {
		defer r.UpdateDispName(myUserID)
	}
	//defer func() {
	//	u.DispNameHash = adler32.Checksum([]byte(u.DispName))
	//}()
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
	U      []*User            // B
	ByID   map[string]*User   // B
	ByName map[string][]*User // B
}

func NewUsers() (us Users) {
	us.U = make([]*User, 0)
	us.ByID = make(map[string]*User, 0)
	us.ByName = make(map[string][]*User, 0)
	return us
}

func (us *Users) Add(id, name string, power int, mem Membership) *User {
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
	return u
}

func (us *Users) Del(u User) {
	// TODO
	// TODO: What if there are two users with the same name?
}

func (us *Users) SetUserName(u User, name string) {
	// TODO
	// TODO: What if there are two users with the same name?
}

type Room struct {
	ID         string     // B
	Name       string     // B
	DispName   string     // B
	CanonAlias string     // B
	Topic      string     // B
	Users      Users      // B
	Msgs       *list.List // B
	myUserID   *string
	UI         interface{}
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

func (r *Room) String() string {
	return r.DispName
}

func (r *Room) UpdateDispName(myUserID string) {
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

func (r *Room) AddUser(userID, username string, power int, membership Membership) (*User, error) {
	//r, ok := rs.ByID[roomID]
	//if !ok {
	//	return nil, fmt.Errorf("Room %v doesn't exist", roomID)
	//}
	u := r.Users.Add(userID, username, power, membership)
	for _, u := range r.Users.U {
		u.UpdateDispName(r, *r.myUserID)
	}
	r.UpdateDispName(*r.myUserID)
	return u, nil
}

func (r *Room) AddUserBatch(userID, username string, power int, membership Membership) error {
	r.Users.Add(userID, username, power, membership)
	return nil
}

func (r *Room) AddUserBatchFinish() {
	for _, u := range r.Users.U {
		u.UpdateDispName(r, "")
	}
	r.UpdateDispName(*r.myUserID)
}

//func (r *Room) AddMessage(msgType string, ts int64, userID, body string) error {
//	//_, ok = r.Users.ByID[userID]
//	//if !ok {
//	//	// We tolerate messages from non existing users.  We take care
//	//	// in printMessage of that case
//	//	AddConsoleMessage(fmt.Sprintf("AddMessage: User %v doesn't exist in room %v",
//	//		userID, roomID))
//	//}
//	m := Message{msgType, ts, userID, body}
//	r.Msgs.PushBack(m)
//	return nil
//}

//func (r *Rooms) PushFrontMessage(msgType string, ts int64, userID, body string) error {
//	m := Message{msgType, ts, userID, body}
//	// Deliberately doesn't reprint the room because this is DEBUG
//	r.Msgs.PushFront(m)
//	return nil
//}

type Rooms struct {
	R                  []*Room            // B
	ByID               map[string]*Room   // B
	ByName             map[string][]*Room // B
	ConsoleRoom        *Room              // B
	consoleRoomID      string
	ConsoleDisplayName string
	ConsoleUserID      string
}

func NewRooms() (rs Rooms) {
	rs.R = make([]*Room, 0)
	rs.ByID = make(map[string]*Room, 0)
	rs.ByName = make(map[string][]*Room, 0)
	return rs
}

func (rs *Rooms) Add(myUserID *string, roomID, name, canonAlias, topic string) (*Room, error) {
	_, ok := rs.ByID[roomID]
	if ok {
		return nil, fmt.Errorf("Room %v already exists", roomID)
	}
	r := NewRoom(roomID, name, canonAlias, topic)
	r.myUserID = myUserID
	r.UpdateDispName(*r.myUserID)
	rs.R = append(rs.R, r)
	rs.ByID[r.ID] = r
	if r.Name != "" {
		if rs.ByName[r.Name] == nil {
			rs.ByName[r.Name] = make([]*Room, 0)
		}
		rs.ByName[r.Name] = append(rs.ByName[r.Name], r)
	}
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
	//rs.UpdateShortcuts()
	return r, nil
}

func (rs *Rooms) SetRoomName(r Room, name string) {
	// TODO
	// TODO: What if there are two rooms with the same name?
}

func (rs *Rooms) AddConsoleMessage(body string) {
	rs.ConsoleRoom.Msgs.PushBack(Message{"m.text", time.Now().Unix() * 1000, ConsoleUserID, body})
}
