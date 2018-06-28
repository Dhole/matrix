package matrixolm

import (
	//"encoding/json"
	"fmt"
	mat "github.com/Dhole/gomatrix"
	"github.com/mitchellh/mapstructure"
	olm "gitlab.com/dhole/go-olm"
	"log"
	"os"
	"strconv"
	"strings"
	"time"
)

var _userID = mat.UserID("@ray_test:matrix.org")
var _username = "ray_test"
var _homeserver = "https://matrix.org"
var _password = ""
var _deviceID = mat.DeviceID("5un3HpnWE04")
var _deviceDisplayName = "go-olm-dev02"

//type EncryptionAlg string
//
//const (
//	EncryptionAlgNone   EncryptionAlg = ""
//	EncryptionAlgOlm    EncryptionAlg = "m.olm.v1.curve25519-aes-sha2"
//	EncryptionAlgMegolm EncryptionAlg = "m.megolm.v1.aes-sha2"
//)

//type RoomID string
//type UserID string
//type SessionID string
//type DeviceID string

//var container *Container

//var cli *mat.Client

func mapUnmarshal(input interface{}, output interface{}) error {
	config := &mapstructure.DecoderConfig{
		TagName: "json",
		Result:  output,
	}
	decoder, err := mapstructure.NewDecoder(config)
	if err != nil {
		panic(err)
	}
	return decoder.Decode(input)
}

// Container contains data structures that are completely or partly permanent
// (backed by a data base).
type Container struct {
	Me         *MyUserDevice
	users      map[mat.UserID]*UserDevices
	sessionsID *RoomsSessionsID
	rooms      map[mat.RoomID]*Room
	db         Databaser
}

// Room gets or creates (if it doesn't exist) a Room and returns it
func (c *Container) Room(roomID mat.RoomID) (room *Room) {
	room = c.rooms[roomID]
	if room == nil {
		room = c.NewRoom(roomID)
		c.rooms[roomID] = room
	}
	return
}

func (c *Container) ForEachUser(fn func(uID mat.UserID, ud *UserDevices) error) (err error) {
	for userID, userDevices := range c.users {
		if err = fn(userID, userDevices); err != nil {
			break
		}
	}
	return err
}

func (c *Container) ForEachRoom(fn func(rID mat.RoomID, r *Room) error) (err error) {
	for roomID, room := range c.rooms {
		if err = fn(roomID, room); err != nil {
			break
		}
	}
	return err
}

// LoadContainer loads the Container datastructure.  It sets up the database and loads
// its contents into the rest of the datastructure.
func LoadContainer(userID mat.UserID, deviceID mat.DeviceID, db Databaser) (*Container, error) {
	var container Container
	container.db = db

	// Load/Create myUser olm data
	container.db.AddMyUserMyDevice(userID, deviceID)
	if !container.db.ExistsOlmAccount(userID, deviceID) {
		olmAccount := olm.NewAccount()
		container.db.StoreOlmAccount(userID, deviceID,
			olmAccount)
	}
	var err error
	// Load stored self device (olm account, ...)
	container.Me, err = container.db.LoadMyUserDevice(userID)
	if err != nil {
		return nil, err
	}

	// Load stored user devices olm data (userID, deviceID, keys, olm sessions, ...)
	container.users, err = container.db.LoadAllUserDevices()
	if err != nil {
		return nil, err
	}

	// Load stored rooms olm data (encryption Algorithm, ...)
	container.rooms, err = container.db.LoadRooms()
	if err != nil {
		return nil, err
	}

	// Load map of olm sessionID by (roomID, userID, deviceID)
	container.sessionsID, err = container.db.LoadMapSessionsID()
	if err != nil {
		return nil, err
	}

	return &container, nil
}

func (c *Container) Close() {
	if c.db != nil {
		c.db.Close()
	}
}

// TODO: On network errors the function that returns the error should be called
// again after some time.  But there are other kind of errors that are final
// (like when a user hasn't uploaded the keys for their device, so we don't
// obtain the keys for their device).  This means that we should parse the
// errors to decide wether to retry or not!

type Device struct {
	user       *UserDevices
	id         mat.DeviceID
	ed25519    olm.Ed25519    // SigningKey
	curve25519 olm.Curve25519 // IdentityKey
	//OneTimeKey       string                              // IdentityKey
	OlmSessions        map[olm.SessionID]*olm.Session
	MegolmInSessions   map[olm.SessionID]*olm.InboundGroupSession
	sharedMegolmOutKey map[olm.SessionID]bool
}

func (d *Device) String() string {
	return fmt.Sprintf("%s:%s", d.ID(), d.curve25519)
}

func (d *Device) UserID() mat.UserID {
	return d.user.ID()
}

func (d *Device) ID() mat.DeviceID {
	return d.id
}

func (d *Device) Curve25519() olm.Curve25519 {
	return d.curve25519
}

func (d *Device) Ed25519() olm.Ed25519 {
	return d.ed25519
}

//func NewDevice(deviceID mat.DeviceID) *Device {
//	return &Device{
//		ID:               mat.DeviceID(deviceID),
//		OlmSessions:      make(map[olm.SessionID]*olm.Session),
//		MegolmInSessions: make(map[olm.SessionID]*olm.InboundGroupSession),
//	}
//}

func SendToDeviceRoomID(key olm.Curve25519) mat.RoomID {
	return mat.RoomID(fmt.Sprintf("_SendToDevice_%s", key))
}

type MyDevice struct {
	ID         mat.DeviceID
	ed25519    olm.Ed25519    // Ed25519
	curve25519 olm.Curve25519 // IdentityKey
	OlmAccount *olm.Account
	//OlmSessions       map[string]*olm.Session              // key:room_id
	MegolmOutSessions map[mat.RoomID]*olm.OutboundGroupSession
}

type UserDevices struct {
	id                mat.UserID
	Devices           map[olm.Curve25519]*Device
	DevicesByID       map[mat.DeviceID]*Device
	devicesTracking   bool
	devicesOutdated   bool
	devicesLastUpdate int64
}

func NewUserDevices(userID mat.UserID) *UserDevices {
	return &UserDevices{
		id:          userID,
		Devices:     make(map[olm.Curve25519]*Device),
		DevicesByID: make(map[mat.DeviceID]*Device),
	}
}

func (ud *UserDevices) ID() mat.UserID {
	return ud.id
}

func (ud *UserDevices) ForEach(fn func(k olm.Curve25519, d *Device) error) (err error) {
	for key, device := range ud.Devices {
		if err = fn(key, device); err != nil {
			break
		}
	}
	return err
}

type MyUserDevice struct {
	ID     mat.UserID
	Device *MyDevice
}

type SessionsID struct {
	olmSessionID      olm.SessionID
	megolmInSessionID olm.SessionID
}

type Room struct {
	id   mat.RoomID
	name string // TODO: Delete

	joined  map[mat.UserID]*User
	invited map[mat.UserID]*User
	left    map[mat.UserID]*User
	banned  map[mat.UserID]*User

	encryptionAlg olm.Algorithm
	//MegolmOutSession *olm.OutboundGroupSession
}

func newRoom(roomID mat.RoomID) *Room {
	return &Room{
		id:      roomID,
		joined:  make(map[mat.UserID]*User),
		invited: make(map[mat.UserID]*User),
		left:    make(map[mat.UserID]*User),
		banned:  make(map[mat.UserID]*User),
	}
}

func (c *Container) NewRoom(roomID mat.RoomID) *Room {
	// TODO: Store in db
	return newRoom(roomID)
}

func (r *Room) ID() mat.RoomID {
	return r.id
}

//func (r *Room) EncryptionAlg() olm.Algorithm {
//	return r.encryptionAlg
//}
//
//func (r *Room) SetOlmEncryption() error {
//	return r.SetEncryption(olm.AlgorithmOlmV1)
//}
//
//func (r *Room) SetMegolmEncryption() error {
//	return r.SetEncryption(olm.AlgorithmMegolmV1)
//}

//func (r *Room) SetEncryption(encryptionAlg olm.Algorithm) error {
//	if r.encryptionAlg == olm.AlgorithmNone {
//		_, err := cli.SendStateEvent(string(r.id), "m.room.encryption", "",
//			map[string]string{"algorithm": string(encryptionAlg)})
//		if err == nil {
//			r.encryptionAlg = encryptionAlg
//			container.db.StoreRoomEncryptionAlg(r.id, encryptionAlg)
//		}
//		return err
//	} else {
//		return fmt.Errorf("The room %v already has the encryption algorithm %v set",
//			r.id, r.encryptionAlg)
//	}
//}

func (r *Room) users(mem Membership) *map[mat.UserID]*User {
	switch mem {
	case MemInvite:
		return &r.invited
	case MemJoin:
		return &r.joined
	case MemLeave:
		return &r.left
	case MemBan:
		return &r.banned
	}
	return nil
}

func (r *Room) ForEachUser(mem Membership, fn func(uID mat.UserID, u *User) error) (err error) {
	for userID, user := range *r.users(mem) {
		if err = fn(userID, user); err != nil {
			break
		}
	}
	return err
}

type Membership int

const (
	MemInvite     Membership = iota
	MemJoin       Membership = iota
	MemLeave      Membership = iota
	MemBan        Membership = iota
	MembershipLen Membership = iota
)

func (r *Room) popUser(userID mat.UserID) (user *User) {
	for _, mem := range []Membership{MemInvite, MemJoin, MemLeave, MemBan} {
		users := r.users(mem)
		if user, ok := (*users)[userID]; ok {
			delete(*users, userID)
			return user
		}
	}
	return nil
	//if user, ok := r.invited[userID]; ok {
	//	delete(r.invited, userID)
	//	return user
	//}
	//if user, ok := r.joined[userID]; ok {
	//	delete(r.joined, userID)
	//	return user
	//}
	//if user, ok := r.left[userID]; ok {
	//	delete(r.left, userID)
	//	return user
	//}
	//if user, ok := r.banned[userID]; ok {
	//	delete(r.banned, userID)
	//	return user
	//}
	//return nil
}

func (r *Room) SetUserMembership(userID mat.UserID, mem Membership) {
	user := r.popUser(userID)
	if user == nil {
		user = NewUser(userID)
	}
	switch mem {
	case MemInvite:
		r.invited[userID] = user
	case MemJoin:
		r.joined[userID] = user
	case MemLeave:
		r.left[userID] = user
	case MemBan:
		r.banned[userID] = user
	}
}

type SendEncEventError struct {
	UserID   mat.UserID
	DeviceID mat.DeviceID
	//Content  interface{}
	Err error
}

func (err *SendEncEventError) Error() string {
	return fmt.Sprintf("Error sending message to user %s [device %s]: %s",
		err.UserID, err.DeviceID, err.Err)
}

type SendEncEventErrors []SendEncEventError

func (errs SendEncEventErrors) Error() string {
	var b []byte
	for _, v := range errs {
		b = append(b, v.Error()...)
		b = append(b, '\n')
	}
	return string(b)
}

//func (r *Room) sendPlaintextMsg(eventType string, contentJSON interface{}) error {
//	_, err := cli.SendMessageEvent(string(r.id), eventType, contentJSON)
//	return err
//	//return cli.SendMessageEvent(roomID, "m.room.message",
//	//	TextMessage{"m.text", text})
//}

type User struct {
	id      mat.UserID
	name    string // TODO: Delete this
	devices *UserDevices
}

func NewUser(userID mat.UserID) *User {
	return &User{id: userID}
}

func (u *User) ID() mat.UserID {
	return u.id
}

type SessionTriplet struct {
	RoomID mat.RoomID
	UserID mat.UserID
	Key    olm.Curve25519
}

// RoomsSessionsID maps (RoomID, UserID, Curve25519) to (olmSessionID, megolmInSessionID)
type RoomsSessionsID struct {
	Sessions map[SessionTriplet]*SessionsID
}

func (rs *RoomsSessionsID) getSessionsID(roomID mat.RoomID, userID mat.UserID,
	key olm.Curve25519) *SessionsID {
	sessionsID, ok := rs.Sessions[SessionTriplet{roomID, userID, key}]
	if !ok {
		return nil
	}
	return sessionsID
}

//func (rs *RoomsSessionsID) makeSessionsID(roomID mat.RoomID, userID mat.UserID,
//	key olm.Curve25519) *SessionsID {
//	room, ok := rs.roomIDuserIDKey[roomID]
//	if !ok {
//		rs.roomIDuserIDKey[roomID] = make(map[mat.UserID]map[olm.Curve25519]*SessionsID)
//		room = rs.roomIDuserIDKey[roomID]
//	}
//	user, ok := room[userID]
//	if !ok {
//		room[userID] = make(map[olm.Curve25519]*SessionsID)
//		user = room[userID]
//	}
//	sessionsID, ok := user[key]
//	if !ok {
//		user[key] = &SessionsID{}
//		sessionsID = user[key]
//	}
//	return sessionsID
//}

func (rs *RoomsSessionsID) makeSessionsID(roomID mat.RoomID, userID mat.UserID,
	key olm.Curve25519) *SessionsID {
	var sessionsID *SessionsID
	sessionsID, ok := rs.Sessions[SessionTriplet{roomID, userID, key}]
	if !ok {
		sessionsID = &SessionsID{}
		rs.Sessions[SessionTriplet{roomID, userID, key}] = sessionsID
	}
	return sessionsID
}

func (rs *RoomsSessionsID) GetOlmSessionID(roomID mat.RoomID, userID mat.UserID, key olm.Curve25519) olm.SessionID {
	sessionsID := rs.getSessionsID(roomID, userID, key)
	if sessionsID == nil {
		return ""
	} else {
		return sessionsID.olmSessionID
	}
}

//func (rs *RoomsSessionsID) GetOlmSession(roomID mat.RoomID, userID mat.UserID, key olm.Curve25519) *olm.Session {
//
//}

func (rs *RoomsSessionsID) setOlmSessionID(roomID mat.RoomID, userID mat.UserID,
	key olm.Curve25519, sessionID olm.SessionID) {
	sessionsID := rs.makeSessionsID(roomID, userID, key)
	sessionsID.olmSessionID = sessionID
}

//func StoreNewOlmSession(roomID mat.RoomID, userID mat.UserID, device *Device, session *olm.Session) {
//	container.sessionsID.setOlmSessionID(roomID, userID, device.Curve25519, session.ID())
//	container.db.StoreOlmSessionID(roomID, userID, device.Curve25519, session.ID())
//	device.OlmSessions[session.ID()] = session
//	container.db.StoreOlmSession(userID, device.ID, session)
//}

func (rs *RoomsSessionsID) setMegolmSessionID(roomID mat.RoomID, userID mat.UserID,
	key olm.Curve25519, sessionID olm.SessionID) {
	sessionsID := rs.makeSessionsID(roomID, userID, key)
	sessionsID.megolmInSessionID = sessionID
}

type SignedKey struct {
	Key        olm.Curve25519               `json:"key"`
	Signatures map[string]map[string]string `json:"signatures"`
}

func SplitAlgorithmKeyID(algorithmKeyID string) (string, string) {
	algorithmKeyIDSlice := strings.Split(algorithmKeyID, ":")
	if len(algorithmKeyIDSlice) != 2 {
		return "", ""
	}
	return algorithmKeyIDSlice[0], algorithmKeyIDSlice[1]
}

func _main() {
	_password = os.Args[1]

	// Load/Create database
	db, err := OpenCryptoDB("test.db")
	if err != nil {
		panic(err)
	}
	container, err := LoadContainer(_userID, _deviceID, db)
	if err != nil {
		container.Close()
		log.Fatal(err)
	}
	defer container.Close()

	cli, _ := mat.NewClient(_homeserver, "", "")
	cli.Prefix = "/_matrix/client/unstable"
	fmt.Println("Logging in...")
	resLogin, err := cli.Login(&mat.ReqLogin{
		Type:                     "m.login.password",
		User:                     _username,
		Password:                 _password,
		DeviceID:                 string(_deviceID),
		InitialDeviceDisplayName: _deviceDisplayName,
	})
	if err != nil {
		panic(err)
	}
	cli.SetCredentials(resLogin.UserID, resLogin.AccessToken)

	//err = cli.SendToDevice("m.room.message", &mat.SendToDeviceMessages{
	//	Messages: map[string]map[string]interface{}{
	//		"@dhole:matrix.org": map[string]interface{}{
	//			"XWYXQDYXNI": map[string]string{"msg": "hello"},
	//		},
	//	}})
	//if err != nil {
	//	panic(err)
	//}
	//return

	//resp, err := cli.KeysQuery(map[string][]string{string(container.me.ID): []string{}}, -1)
	//if err != nil {
	//	panic(err)
	//}
	//fmt.Printf("\n\n%+v\n\n", resp)
	//return

	//err = cli.UpdateDevice(string(container.me.Device.ID), "go-olm-test")
	//if err != nil {
	//	panic(err)
	//}

	//err = container.me.KeysUpload()
	//if err != nil {
	//	panic(err)
	//}
	//return

	joinedRooms, err := cli.JoinedRooms()
	if err != nil {
		panic(err)
	}
	for i := 0; i < 2; i++ {
		//	for i := 0; i < len(joinedRooms.JoinedRooms); i++ {
		roomID := mat.RoomID(joinedRooms.JoinedRooms[i])
		if container.rooms[roomID] == nil {
			room := container.NewRoom(roomID)
			room.encryptionAlg = olm.AlgorithmNone
			container.rooms[roomID] = room
		}
		room := container.rooms[roomID]
		joinedMembers, err := cli.JoinedMembers(string(roomID))
		if err != nil {
			panic(err)
		}
		for userID, userDetails := range joinedMembers.Joined {
			name := ""
			if userDetails.DisplayName != nil {
				name = *userDetails.DisplayName
			}
			room.joined[mat.UserID(userID)] = &User{
				id:   mat.UserID(userID),
				name: name,
			}
		}
		if len(joinedMembers.Joined) < 2 {
			continue
		}
		fmt.Printf("%02d %s\n", i, roomID)
		count := 0
		for userID, user := range room.joined {
			if userID == container.Me.ID {
				continue
			}
			count++
			if count == 6 && len(room.joined) > 6 {
				fmt.Printf("\t...\n")
				continue
			} else if count > 6 {
				continue
			}
			if user.name != "" {
				fmt.Printf("\t%s (%s)\n", user.name, userID)
			} else {
				fmt.Printf("\t%s\n", userID)
			}
		}
		fmt.Printf("\n")
	}

	//theirUser := &UserDevices{
	//	//ID:      "@dhole:matrix.org",
	//	Devices: make(map[DeviceID]*Device),
	//}

	fmt.Printf("Select room number or press enter for new room: ")
	var input string
	var roomID mat.RoomID
	var theirUser User
	// TMP
	//fmt.Scanln(&input)
	fmt.Println()
	roomIdx, err := strconv.Atoi(input)
	if err != nil {
		fmt.Println("Creating new room...")
		fmt.Printf("Write user ID to invite: ")
		// TMP
		//fmt.Scanln(&input)
		fmt.Println()
		input = "@dhole:matrix.org"
		if input != "" {
			theirUser.id = mat.UserID(input)
		}
		resp, err := cli.CreateRoom(&mat.ReqCreateRoom{
			Invite: []string{string(theirUser.id)},
		})
		if err != nil {
			panic(err)
		}
		roomID = mat.RoomID(resp.RoomID)
		container.rooms[roomID] = container.NewRoom(mat.RoomID(roomID))
	} else {
		roomID = mat.RoomID(joinedRooms.JoinedRooms[roomIdx])
		fmt.Println("Selected room is", roomID)
		for userID, _ := range container.rooms[mat.RoomID(roomID)].joined {
			if userID == container.Me.ID {
				continue
			}
			theirUser.id = userID
			break
		}
	}

	room := container.rooms[roomID]

	// TMP
	room.joined[theirUser.id] = &User{
		id: theirUser.id,
	}

	//theirUserDevices, err := theirUser.Devices()
	//if err != nil {
	//	panic(err)
	//}

	//deviceKeysAlgorithms := map[string]map[string]string{string(theirUserDevices.ID): map[string]string{}}
	//keysToClaim := 0
	//for theirDeviceID, _ := range theirUserDevices.Devices {
	//	if container.sessionsID.GetOlmSessionID(room.id, theirUserDevices.ID, theirDeviceID) == "" {
	//		deviceKeysAlgorithms[string(theirUserDevices.ID)][string(theirDeviceID)] = "signed_curve25519"
	//		keysToClaim++
	//	}
	//}

	//if keysToClaim > 0 {
	//	fmt.Printf("%+v\n", deviceKeysAlgorithms)
	//	respClaim, err := cli.KeysClaim(deviceKeysAlgorithms, -1)
	//	if err != nil {
	//		panic(err)
	//	}
	//	fmt.Printf("%+v\n", respClaim)

	//	var oneTimeKey string
	//	for theirDeviceID, _ := range deviceKeysAlgorithms[string(theirUserDevices.ID)] {
	//		algorithmKey, ok := respClaim.OneTimeKeys[string(theirUserDevices.ID)][theirDeviceID]
	//		if !ok {
	//			panic(fmt.Sprint("One time key for device", theirDeviceID, "not returned"))
	//		}
	//		for algorithmKeyID, rawOTK := range algorithmKey {
	//			algorithm, _ := SplitAlgorithmKeyID(algorithmKeyID)
	//			switch algorithm {
	//			case "signed_curve25519":
	//				var OTK SignedKey
	//				err := mapstructure.Decode(rawOTK, &OTK)
	//				if err != nil {
	//					panic(err)
	//				}
	//				//fmt.Printf("OTK: %+v\n", OTK)
	//				device, ok := theirUserDevices.Devices[DeviceID(theirDeviceID)]
	//				if ok {
	//					oneTimeKey = OTK.Key
	//					olmSession, err := container.me.Device.OlmAccount.NewOutboundSession(device.Curve25519,
	//						oneTimeKey)
	//					if err != nil {
	//						panic(err)
	//					}
	//					container.sessionsID.setOlmSessionID(room.id, theirUserDevices.ID, device.ID, SessionID(olmSession.ID()))
	//					container.db.StoreOlmSessionID(room.id, theirUserDevices.ID, device.ID, SessionID(olmSession.ID()))
	//					device.OlmSessions[SessionID(olmSession.ID())] = olmSession
	//					container.db.StoreOlmSession(theirUserDevices.ID, device.ID, olmSession)
	//				}
	//			}
	//		}
	//	}
	//	fmt.Printf("%+v\n", theirUserDevices)
	//	for _, device := range theirUserDevices.Devices {
	//		fmt.Printf("%+v\n", *device)
	//	}
	//}

	//if room.EncryptionAlg() != olm.AlgorithmOlmV1 {
	//	err = room.SetOlmEncryption()
	//	if err != nil {
	//		panic(err)
	//	}
	//}

	//if room.EncryptionAlg() == olm.AlgorithmNone {
	//	err := room.SetMegolmEncryption()
	//	if err != nil {
	//		panic(err)
	//	}
	//}

	//text := fmt.Sprint("I'm encrypted :D ~ ", time.Now().Format("2006-01-02 15:04:05"))
	//err = room.SendText(text)
	//err = room.SendText(text)
	if err != nil {
		// TODO: Do type assertion to extract each SendEncEventError?
		log.Println(err)
	}

	res := &mat.RespSync{}
	for {
		res, err = cli.SyncRequest(30000, res.NextBatch, "", false, "online")
		if err != nil {
			time.Sleep(10)
			continue
		}
		//Filter(res, roomID)
	}
}

//func Filter(res *mat.RespSync, myRoomID mat.RoomID) {
//	for roomID, roomData := range res.Rooms.Join {
//		if roomID != string(myRoomID) {
//			continue
//		}
//		//fmt.Printf("\t%s\n", roomID)
//		for _, ev := range roomData.Timeline.Events {
//			ev.RoomID = roomID
//			sender, body := parseEvent(&ev)
//			fmt.Printf("> [%s] %s %s\n",
//				time.Unix(ev.Timestamp/1000, 0).Format("2006-01-02 15:04"),
//				sender, body)
//		}
//	}
//	for roomID, _ := range res.Rooms.Invite {
//		_, err := cli.JoinRoom(roomID, "", nil)
//		if err != nil {
//			fmt.Printf("Err Couldn't auto join room %s\n", roomID)
//		} else {
//			fmt.Printf("INFO Autojoined room %s\n", roomID)
//		}
//	}
//	for _, ev := range res.ToDevice.Events {
//		sender, body := parseSendToDeviceEvent(&ev)
//		fmt.Printf("$$$ %s %s\n", sender, body)
//	}
//}

// TODO: Delete this
type Ciphertext struct {
	Type olm.MsgType `json:"type"`
	Body string      `json:"body"`
}

type OlmMsg struct {
	Algorithm  olm.Algorithm  `json:"algorithm"`
	SenderKey  olm.Curve25519 `json:"sender_key"`
	Ciphertext map[olm.Curve25519]struct {
		Type olm.MsgType `json:"type"`
		Body string      `json:"body"`
	} `json:"ciphertext"`
}

type RoomKey struct {
	// OlmSenderKey is filled when this event is obtained through the
	// decryption of an m.room.encrypted event.
	OlmSenderKey olm.Curve25519
	Algorithm    olm.Algorithm `json:"algorithm"`
	RoomID       mat.RoomID    `json:"room_id"`
	SessionID    olm.SessionID `json:"session_id"`
	SessionKey   string        `json:"session_key"`
}

type MegolmMsg struct {
	Algorithm  olm.Algorithm  `json:"algorithm"`
	Ciphertext string         `json:"ciphertext"`
	DeviceID   mat.DeviceID   `json:"device_id"`
	SenderKey  olm.Curve25519 `json:"sender_key"`
	SessionID  olm.SessionID  `json:"session_id"`
}

func parseEvent(ev *Event) (sender string, body string) {
	sender = fmt.Sprintf("%s:", ev.Sender)
	//	userID := mat.UserID(ev.Sender)
	//	roomID := mat.RoomID(ev.RoomID)
	switch ev.Type {
	case "m.room.message":
		switch ev.Content["msgtype"] {
		case "m.text":
		case "m.emote":
			sender = fmt.Sprintf("* %s", ev.Sender)
		case "m.notice":
			sender = fmt.Sprintf("%s ~", ev.Sender)
		}
		body, _ = ev.Content["body"].(string)
	// DONE
	//case "m.room.encrypted":
	//	sender, body = parseRoomEncrypted(roomID, userID, ev.Content)
	// DONE
	//case "m.room_key":
	//	sender, body = parseRoomKey(roomID, userID, ev.Content)
	// TODO: Key sharing requests and forwards:
	// https://docs.google.com/document/d/1m4gQkcnJkxNuBmb5NoFCIadIY-DyqqNAS3lloE73BlQ/edit#
	case "m.room_key_request":
		body = fmt.Sprintf("TODO ~ %s -> %+v", ev.Type, ev.Content)
	case "m.forwarded_room_key":
		body = fmt.Sprintf("TODO ~ %s -> %+v", ev.Type, ev.Content)
	default:
		sender = fmt.Sprintf("[?] %s", sender)
		body = fmt.Sprintf("%s -> %+v", ev.Type, ev.Content)
	}
	return
}

func parseSendToDeviceEvent(_ev *SendToDeviceEvent) (sender string, body string) {
	ev := &Event{Event: mat.Event{Sender: _ev.Sender, Type: _ev.Type, Content: _ev.Content}}
	if ev.Type == "m.room.encrypted" {
		if senderKey, ok := ev.Content["sender_key"]; ok {
			if senderKey, ok := senderKey.(string); ok {
				ev.RoomID = string(SendToDeviceRoomID(olm.Curve25519(senderKey)))
			}
		}
	}
	sender, body = parseEvent(ev)
	return
}

//
//func Update(res *mat.RespSync) {
//	for roomID, roomData := range res.Rooms.Join {
//		//r, _ := c.Rs.Add(&c.cfg.UserID, roomID, MemJoin)
//		for _, ev := range roomData.State.Events {
//			//r.updateState(&ev)
//		}
//		r.PushToken(roomData.Timeline.PrevBatch)
//		for _, ev := range roomData.Timeline.Events {
//			//r.PushEvent(&ev)
//		}
//		//r.PushToken(res.NextBatch)
//	}
//	for roomID, roomData := range res.Rooms.Invite {
//		//r, _ := c.Rs.Add(&c.cfg.UserID, roomID, MemInvite)
//		for _, ev := range roomData.State.Events {
//			//r.updateState(&ev)
//		}
//	}
//}
