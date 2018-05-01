package main

import (
	"encoding/json"
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
var _deviceID = mat.DeviceID("5un3HpnWE01")
var _deviceDisplayName = "go-olm-dev01"

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

var store *Store

var cli *mat.Client

// Store contains data structures that are completely or partly permanent
// (backed by a data base).
type Store struct {
	me         *MyUserDevice
	users      map[mat.UserID]*UserDevices
	sessionsID *RoomsSessionsID
	rooms      map[mat.RoomID]*Room
	db         Storer
}

// LoadStore loads the Store datastructure.  It sets up the database and loads
// its contents into the rest of the datastructure.
func LoadStore(userID mat.UserID, deviceID mat.DeviceID, db Storer) (*Store, error) {
	var store Store
	store.db = db

	// Load/Create myUser olm data
	store.db.AddMyUserMyDevice(userID, deviceID)
	if !store.db.ExistsOlmAccount(userID, deviceID) {
		olmAccount := olm.NewAccount()
		store.db.StoreOlmAccount(userID, deviceID,
			olmAccount)
		// TODO: Upload device identity keys to server using `/keys/upload`
		// TODO: Upload device one-time keys to server using `/keys/upload`
	}
	var err error
	store.me, err = store.db.LoadMyUserDevice(userID)
	if err != nil {
		return nil, err
	}
	fmt.Println("Identity keys:", store.me.Device.Ed25519, store.me.Device.Curve25519)

	// Load stored user devices olm data (userID, deviceID, keys, olm sessions, ...)
	store.users, err = store.db.LoadAllUserDevices()
	if err != nil {
		return nil, err
	}

	// Load stored rooms olm data (encryption Algorithm, ...)
	store.rooms, err = store.db.LoadRooms()
	if err != nil {
		return nil, err
	}

	// Load map of olm sessionID by (roomID, userID, deviceID)
	store.sessionsID, err = store.db.LoadMapSessionsID()
	if err != nil {
		return nil, err
	}

	return &store, nil
}

func (store *Store) Close() {
	if store.db != nil {
		store.db.Close()
	}
}

// TODO: On network errors the function that returns the error should be called
// again after some time.  But there are other kind of errors that are final
// (like when a user hasn't uploaded the keys for their device, so we don't
// obtain the keys for their device).  This means that we should parse the
// errors to decide wether to retry or not!

type Device struct {
	ID         mat.DeviceID
	Ed25519    olm.Ed25519    // SigningKey
	Curve25519 olm.Curve25519 // IdentityKey
	//OneTimeKey       string                              // IdentityKey
	OlmSessions      map[olm.SessionID]*olm.Session
	MegolmInSessions map[olm.SessionID]*olm.InboundGroupSession
}

func NewDevice(deviceID mat.DeviceID) *Device {
	return &Device{
		ID:               mat.DeviceID(deviceID),
		OlmSessions:      make(map[olm.SessionID]*olm.Session),
		MegolmInSessions: make(map[olm.SessionID]*olm.InboundGroupSession),
	}
}

func (d *Device) NewOlmSession(roomID mat.RoomID, userID mat.UserID) (*olm.Session, error) {
	deviceKeysAlgorithms := map[string]map[string]string{
		string(userID): map[string]string{string(d.ID): "signed_curve25519"},
	}
	fmt.Printf("Query: %+v\n", deviceKeysAlgorithms)
	respClaim, err := cli.KeysClaim(deviceKeysAlgorithms, -1)
	if err != nil {
		return nil, err
	}
	fmt.Printf("Response: %+v\n", respClaim)

	var oneTimeKey olm.Curve25519
	// TODO: Check each key individually to verify that the first one
	// exists before getting the second one and avoid the possibility of
	// getting a key from a nil map.
	algorithmKey, ok := respClaim.OneTimeKeys[string(userID)][string(d.ID)]
	if !ok {
		// TODO: This error is final, should we mark the device as
		// unusable?  How do we know when new keys are uploaded?
		// Should we keep trying to claim one time keys every time we
		// send a message?
		return nil, fmt.Errorf("One time key for device %s not returned", d.ID)
	}
	// SECURITY TODO: Verify signatures!
	for algorithmKeyID, rawOTK := range algorithmKey {
		algorithm, _ := SplitAlgorithmKeyID(algorithmKeyID)
		switch algorithm {
		case "signed_curve25519":
			var OTK mat.OneTimeKey
			err := mapstructure.Decode(rawOTK, &OTK)
			if err != nil {
				return nil, err
			}

			oneTimeKey = OTK.Key
			session, err := store.me.Device.OlmAccount.NewOutboundSession(
				d.Curve25519, oneTimeKey)
			if err != nil {
				return nil, err
			}
			StoreNewOlmSession(roomID, userID, d, session)

			return session, nil
		}
	}

	return nil, fmt.Errorf("/keys/claim API didn't return a signed_curve25519 object")
}

// func (d *Device) EncryptOlmMsg(roomID RoomID, userID UserID, eventType string,
// 	contentJSON interface{}) (interface{}, error) {
// 	olmSession, ok := d.OlmSessions[store.sessionsID.GetOlmSessionID(roomID, userID, d.Curve25519)]
// 	if !ok {
// 		var err error
// 		olmSession, err = d.NewOlmSession(roomID, userID)
// 		if err != nil {
// 			return nil, err
// 		}
// 	}
// 	payload := map[string]interface{}{
// 		"type":           eventType,
// 		"content":        contentJSON,
// 		"recipient":      userID,
// 		"sender":         store.me.ID,
// 		"recipient_keys": map[string]string{"ed25519": d.Ed25519},
// 		"room_id":        roomID}
// 	payloadJSON, err := json.Marshal(payload)
// 	if err != nil {
// 		panic(err)
// 	}
// 	fmt.Println(string(payloadJSON))
// 	encryptMsgType, encryptedMsg := olmSession.Encrypt(string(payloadJSON))
// 	store.db.StoreOlmSession(userID, d.ID, olmSession)
//
// 	return map[string]interface{}{
// 		"algorithm": "m.olm.v1.curve25519-aes-sha2",
// 		"ciphertext": map[string]map[string]interface{}{
// 			d.Curve25519: map[string]interface{}{
// 				"type": encryptMsgType,
// 				"body": encryptedMsg,
// 			},
// 		},
// 		//"device_id":  store.me.ID,
// 		"sender_key": store.me.Device.Curve25519,
// 		"session_id": olmSession.ID()}, nil
// }

func (d *Device) EncryptOlmMsg(roomID mat.RoomID, userID mat.UserID, eventType string,
	contentJSON interface{}) (olm.MsgType, string, error) {
	session, ok := d.OlmSessions[store.sessionsID.GetOlmSessionID(roomID,
		userID, d.Curve25519)]
	if !ok {
		var err error
		session, err = d.NewOlmSession(roomID, userID)
		if err != nil {
			return 0, "", err
		}
	}
	payload := map[string]interface{}{
		"type":           eventType,
		"content":        contentJSON,
		"recipient":      userID,
		"sender":         store.me.ID,
		"recipient_keys": map[string]olm.Ed25519{"ed25519": d.Ed25519},
		"room_id":        roomID}
	if strings.HasPrefix(string(roomID), "_SendToDevice") {
		delete(payload, "room_id")
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		panic(err)
	}
	//fmt.Println(string(payloadJSON))
	encryptMsgType, encryptedMsg := session.Encrypt(string(payloadJSON))
	store.db.StoreOlmSession(userID, d.ID, session)

	return encryptMsgType, encryptedMsg, nil
}

func SendToDeviceRoomID(key olm.Curve25519) mat.RoomID {
	return mat.RoomID(fmt.Sprintf("_SendToDevice_%s", key))
}

func (d *Device) SendMegolmOutKey(roomID mat.RoomID, userID mat.UserID,
	session *olm.OutboundGroupSession) error {
	_roomID := SendToDeviceRoomID(d.Curve25519)
	msgType, msg, err := d.EncryptOlmMsg(_roomID, userID, "m.room_key",
		RoomKey{Algorithm: olm.AlgorithmMegolmV1, RoomID: roomID,
			SessionID: session.ID(), SessionKey: session.SessionKey()})
	if err != nil {
		return err
	}
	ciphertext := make(map[olm.Curve25519]Ciphertext)
	ciphertext[d.Curve25519] = Ciphertext{Type: msgType, Body: msg}
	contentJSONEnc := map[string]interface{}{
		"algorithm":  olm.AlgorithmOlmV1,
		"ciphertext": ciphertext,
		"sender_key": store.me.Device.Curve25519}
	log.Println("Sending MegolmOut Key to", userID, d.ID, "for room", roomID, "from", store.me.ID, store.me.Device.ID, "sender_key", store.me.Device.Curve25519, "session_id", session.ID())
	log.Println("SessionKey: ", session.SessionKey())
	err = cli.SendToDevice("m.room.encrypted", &mat.SendToDeviceMessages{
		Messages: map[string]map[string]interface{}{
			string(userID): map[string]interface{}{string(d.ID): contentJSONEnc}}})
	if err != nil {
		return err
	}
	return nil
}

type MyDevice struct {
	ID         mat.DeviceID
	Ed25519    olm.Ed25519    // Ed25519
	Curve25519 olm.Curve25519 // IdentityKey
	OlmAccount *olm.Account
	//OlmSessions       map[string]*olm.Session              // key:room_id
	MegolmOutSessions map[mat.RoomID]*olm.OutboundGroupSession
}

type UserDevices struct {
	ID                mat.UserID
	Devices           map[olm.Curve25519]*Device
	DevicesByID       map[mat.DeviceID]*Device
	devicesTracking   bool
	devicesOutdated   bool
	devicesLastUpdate int64
}

func NewUserDevices(userID mat.UserID) *UserDevices {
	return &UserDevices{
		ID:          userID,
		Devices:     make(map[olm.Curve25519]*Device),
		DevicesByID: make(map[mat.DeviceID]*Device),
	}
}

func (ud *UserDevices) Update() error {
	log.Println("Updating list of user", ud.ID, "devices")
	respQuery, err := cli.KeysQuery(map[string][]string{string(ud.ID): []string{}}, -1)
	if err != nil {
		return err
	}
	//fmt.Printf("%+v\n", respQuery)
	// TODO: Verify signatures, and note who has signed the key
	for theirDeviceID, deviceKeys := range respQuery.DeviceKeys[string(ud.ID)] {
		device := NewDevice(mat.DeviceID(theirDeviceID))
		for algorithmKeyID, key := range deviceKeys.Keys {
			algorithm, theirDeviceID2 := SplitAlgorithmKeyID(algorithmKeyID)
			if theirDeviceID != theirDeviceID2 {
				panic("TODO: Handle this case")
			}
			switch algorithm {
			case "ed25519":
				device.Ed25519 = olm.Ed25519(key)
			case "curve25519":
				device.Curve25519 = olm.Curve25519(key)
			}
		}
		if device.Ed25519 == "" || device.Curve25519 == "" {
			// TODO: Handle this case properly
			continue
		}
		store.db.AddUserDevice(ud.ID, mat.DeviceID(theirDeviceID))
		store.db.StorePubKeys(ud.ID, device.ID, device.Ed25519, device.Curve25519)
		ud.Devices[device.Curve25519] = device
		ud.DevicesByID[device.ID] = device
	}
	return nil
}

type MyUserDevice struct {
	ID     mat.UserID
	Device *MyDevice
}

func (me *MyUserDevice) KeysUpload() error {
	olmAccount := me.Device.OlmAccount
	deviceKeys := mat.DeviceKeys{
		UserID:     me.ID,
		DeviceID:   me.Device.ID,
		Algorithms: []string{"m.olm.curve25519-aes-sha256"},
		Keys: map[string]string{
			fmt.Sprintf("curve25519:%s", me.Device.ID): string(me.Device.Curve25519),
			fmt.Sprintf("ed25519:%s", me.Device.ID):    string(me.Device.Ed25519),
		},
	}
	signedDeviceKeys, err := olmAccount.SignJSON(deviceKeys,
		string(me.ID), string(me.Device.ID))
	if err != nil {
		return err
	}
	fmt.Printf("\n%+v\n\n", signedDeviceKeys)
	err = mapstructure.Decode(signedDeviceKeys, &deviceKeys)
	if err != nil {
		return err
	}
	olmAccount.GenOneTimeKeys(4)
	store.db.StoreOlmAccount(me.ID, me.Device.ID, olmAccount)
	otks := olmAccount.OneTimeKeys()
	oneTimeKeys := make(map[string]mat.OneTimeKey)
	for keyID, key := range otks.Curve25519 {
		otk := mat.OneTimeKey{Key: key}
		signedOtk, err := olmAccount.SignJSON(otk, string(me.ID), string(me.Device.ID))
		if err != nil {
			return err
		}
		err = mapstructure.Decode(signedOtk, &otk)
		if err != nil {
			return err
		}
		oneTimeKeys[fmt.Sprintf("signed_curve25519:%s", keyID)] = otk
	}
	fmt.Printf("\n%+v\n%+v\n", deviceKeys, oneTimeKeys)
	res, err := cli.KeysUpload(&deviceKeys, oneTimeKeys)
	if err != nil {
		return err
	}
	fmt.Printf("\n%+v\n", res)
	olmAccount.MarkKeysAsPublished()
	store.db.StoreOlmAccount(me.ID, me.Device.ID, olmAccount)
	return nil
}

type SessionsID struct {
	olmSessionID      olm.SessionID
	megolmInSessionID olm.SessionID
}

type Room struct {
	id    mat.RoomID
	name  string
	Users map[mat.UserID]*User
	// TODO: encryption type
	encryptionAlg olm.Algorithm
	//MegolmOutSession *olm.OutboundGroupSession
}

func NewRoom(roomID mat.RoomID) *Room {
	return &Room{
		id:    roomID,
		Users: make(map[mat.UserID]*User),
	}
}

func (r *Room) EncryptionAlg() olm.Algorithm {
	return r.encryptionAlg
}

func (r *Room) SetOlmEncryption() error {
	return r.SetEncryption(olm.AlgorithmOlmV1)
}

func (r *Room) SetMegolmEncryption() error {
	return r.SetEncryption(olm.AlgorithmMegolmV1)
}

func (r *Room) SetEncryption(encryptionAlg olm.Algorithm) error {
	if r.encryptionAlg == olm.AlgorithmNone {
		_, err := cli.SendStateEvent(string(r.id), "m.room.encryption", "",
			map[string]string{"algorithm": string(encryptionAlg)})
		if err == nil {
			r.encryptionAlg = encryptionAlg
			store.db.StoreRoomEncryptionAlg(r.id, encryptionAlg)
		}
		return err
	} else {
		return fmt.Errorf("The room %v already has the encryption algorithm %v set",
			r.id, r.encryptionAlg)
	}
}

func (r *Room) SendText(text string) error {
	return r.SendMsg("m.room.message", mat.TextMessage{MsgType: "m.text", Body: text})
}

func (r *Room) SendMsg(eventType string, contentJSON interface{}) error {
	switch r.encryptionAlg {
	case olm.AlgorithmNone:
		return r.sendPlaintextMsg(eventType, contentJSON)
	case olm.AlgorithmOlmV1:
		return r.sendOlmMsg(eventType, contentJSON)
	case olm.AlgorithmMegolmV1:
		return r.sendMegolmMsg(eventType, contentJSON)
	}
	return nil
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

func (r *Room) EncryptMegolmMsg(eventType string, contentJSON interface{}) string {
	session, ok := store.me.Device.MegolmOutSessions[r.id]
	if !ok {
		// TODO: Should we store the initial SessionKey, so that we can
		// decrypt past messages?  What does Riot do?
		session = olm.NewOutboundGroupSession()
		store.me.Device.MegolmOutSessions[r.id] = session
		store.db.StoreMegolmOutSession(store.me.ID, store.me.Device.ID, session)
	}
	payload := map[string]interface{}{
		"type":    eventType,
		"content": contentJSON,
		"sender":  store.me.ID, // TODO: Needed?
		"room_id": r.id}        // TODO: Needed?
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		panic(err)
	}
	//fmt.Println(string(payloadJSON))
	encryptedMsg := session.Encrypt(string(payloadJSON))
	store.db.StoreMegolmOutSession(store.me.ID, store.me.Device.ID, session)

	return encryptedMsg
}

// TODO: It seems I can batch all the encrypted messages into one, identified
// by the Curve25519 key of the device of the user.
func (r *Room) sendOlmMsg(eventType string, contentJSON interface{}) SendEncEventErrors {
	var errs SendEncEventErrors
	ciphertext := make(map[olm.Curve25519]Ciphertext)
	for _, user := range r.Users {
		userDevices, err := user.Devices()
		if err != nil {
			errs = append(errs, SendEncEventError{UserID: user.id, Err: err})
			continue
		}
		// TODO: Batch oneTimeKey claims of all devices for a user into
		// one API call.  For now, device.EncryptOlmMsg will call
		// device.newOlmSession for each device without session, which
		// will trigger a oneTimeKey call for each device.
		for _, device := range userDevices.Devices {
			msgType, body, err := device.EncryptOlmMsg(r.id, user.id, eventType, contentJSON)
			if err != nil {
				errs = append(errs, SendEncEventError{UserID: user.id,
					DeviceID: device.ID, Err: err})
				continue
			}
			ciphertext[device.Curve25519] = Ciphertext{Type: msgType, Body: body}
		}
	}
	contentJSONEnc := map[string]interface{}{
		"algorithm":  olm.AlgorithmOlmV1,
		"ciphertext": ciphertext,
		"sender_key": store.me.Device.Curve25519}
	_, err := cli.SendMessageEvent(string(r.id), "m.room.encrypted", contentJSONEnc)
	if err != nil {
		errs = append(errs, SendEncEventError{Err: err})
	}
	return errs
}

func (r *Room) sendMegolmMsg(eventType string, contentJSON interface{}) SendEncEventErrors {
	var errs SendEncEventErrors
	// TODO: Get Megolm SessionKey before encrypting the first message,
	// otherwise the SessionKey ratchet will have advanced and can't be
	// used to decrypt the message.
	ciphertext := r.EncryptMegolmMsg(eventType, contentJSON)
	session, _ := store.me.Device.MegolmOutSessions[r.id]
	for _, user := range r.Users {
		userDevices, err := user.Devices()
		if err != nil {
			errs = append(errs, SendEncEventError{UserID: user.id, Err: err})
			continue
		}
		for _, device := range userDevices.Devices {
			device.SendMegolmOutKey(r.id, user.id, session)
		}
	}
	contentJSONEnc := map[string]interface{}{
		"algorithm":  olm.AlgorithmMegolmV1,
		"ciphertext": ciphertext,
		"sender_key": store.me.Device.Curve25519,
		"session_id": session.ID(),
		"device_id":  store.me.Device.ID}
	//log.Println("Join the room now...")
	//time.Sleep(10 * time.Second)
	_, err := cli.SendMessageEvent(string(r.id), "m.room.encrypted", contentJSONEnc)
	if err != nil {
		errs = append(errs, SendEncEventError{Err: err})
	}
	return errs
}

func (r *Room) sendPlaintextMsg(eventType string, contentJSON interface{}) error {
	_, err := cli.SendMessageEvent(string(r.id), eventType, contentJSON)
	return err
	//return cli.SendMessageEvent(roomID, "m.room.message",
	//	TextMessage{"m.text", text})
}

type User struct {
	id   mat.UserID
	name string
	//devices *UserDevices
}

// Blocks when calling userDevices.Update()
// TODO: Handle tracking and outdated devices
func (u *User) Devices() (*UserDevices, error) {
	return GetUserDevices(u.id)
}

func GetUserDevices(userID mat.UserID) (*UserDevices, error) {
	userDevices, ok := store.users[userID]
	if ok {
		return userDevices, nil
	} else {
		var userDevices *UserDevices
		userDevices = NewUserDevices(userID)
		store.users[userDevices.ID] = userDevices
		store.db.AddUser(userDevices.ID)

		if err := userDevices.Update(); err != nil {
			return nil, err
		}
		return userDevices, nil
	}
}

func GetUserDevice(userID mat.UserID, deviceKey olm.Curve25519) (*Device, error) {
	userDevices, err := GetUserDevices(userID)
	if err != nil {
		return nil, err
	}
	device := userDevices.Devices[deviceKey]
	if device == nil {
		return nil, fmt.Errorf("Device with key %s for user %s not available",
			deviceKey, userID)
	}
	return device, nil
}

// RoomsSessionsID maps (RoomID, UserID, Curve25519) to (olmSessionID, megolmInSessionID)
type RoomsSessionsID struct {
	roomIDuserIDKey map[mat.RoomID]map[mat.UserID]map[olm.Curve25519]*SessionsID
}

func (rs *RoomsSessionsID) getSessionsID(roomID mat.RoomID, userID mat.UserID,
	key olm.Curve25519) *SessionsID {
	room, ok := rs.roomIDuserIDKey[roomID]
	if !ok {
		return nil
	}
	user, ok := room[userID]
	if !ok {
		return nil
	}
	sessionsID, ok := user[key]
	if !ok {
		return nil
	}
	return sessionsID
}

func (rs *RoomsSessionsID) makeSessionsID(roomID mat.RoomID, userID mat.UserID,
	key olm.Curve25519) *SessionsID {
	room, ok := rs.roomIDuserIDKey[roomID]
	if !ok {
		rs.roomIDuserIDKey[roomID] = make(map[mat.UserID]map[olm.Curve25519]*SessionsID)
		room = rs.roomIDuserIDKey[roomID]
	}
	user, ok := room[userID]
	if !ok {
		room[userID] = make(map[olm.Curve25519]*SessionsID)
		user = room[userID]
	}
	sessionsID, ok := user[key]
	if !ok {
		user[key] = &SessionsID{}
		sessionsID = user[key]
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

func StoreNewOlmSession(roomID mat.RoomID, userID mat.UserID, device *Device, session *olm.Session) {
	store.sessionsID.setOlmSessionID(roomID, userID, device.Curve25519, session.ID())
	store.db.StoreOlmSessionID(roomID, userID, device.Curve25519, session.ID())
	device.OlmSessions[session.ID()] = session
	store.db.StoreOlmSession(userID, device.ID, session)
}

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

func main() {
	_password = os.Args[1]

	// Load/Create database
	db, err := OpenCryptoDB("test.db")
	if err != nil {
		panic(err)
	}
	store, err = LoadStore(_userID, _deviceID, db)
	if err != nil {
		store.Close()
		log.Fatal(err)
	}
	defer store.Close()

	cli, _ = mat.NewClient(_homeserver, "", "")
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

	//resp, err := cli.KeysQuery(map[string][]string{string(store.me.ID): []string{}}, -1)
	//if err != nil {
	//	panic(err)
	//}
	//fmt.Printf("\n\n%+v\n\n", resp)
	//return

	//err = cli.UpdateDevice(string(store.me.Device.ID), "go-olm-test")
	//if err != nil {
	//	panic(err)
	//}

	err = store.me.KeysUpload()
	if err != nil {
		panic(err)
	}
	//return

	joinedRooms, err := cli.JoinedRooms()
	if err != nil {
		panic(err)
	}
	for i := 0; i < 2; i++ {
		//	for i := 0; i < len(joinedRooms.JoinedRooms); i++ {
		roomID := mat.RoomID(joinedRooms.JoinedRooms[i])
		if store.rooms[roomID] == nil {
			room := NewRoom(roomID)
			room.encryptionAlg = olm.AlgorithmNone
			store.rooms[roomID] = room
		}
		room := store.rooms[roomID]
		joinedMembers, err := cli.JoinedMembers(string(roomID))
		if err != nil {
			panic(err)
		}
		for userID, userDetails := range joinedMembers.Joined {
			name := ""
			if userDetails.DisplayName != nil {
				name = *userDetails.DisplayName
			}
			room.Users[mat.UserID(userID)] = &User{
				id:   mat.UserID(userID),
				name: name,
			}
		}
		if len(joinedMembers.Joined) < 2 {
			continue
		}
		fmt.Printf("%02d %s\n", i, roomID)
		count := 0
		for userID, user := range room.Users {
			if userID == store.me.ID {
				continue
			}
			count++
			if count == 6 && len(room.Users) > 6 {
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
	roomIdx, err := strconv.Atoi(input)
	if err != nil {
		fmt.Println("Creating new room...")
		fmt.Printf("Write user ID to invite: ")
		// TMP
		//fmt.Scanln(&input)
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
		store.rooms[roomID] = NewRoom(mat.RoomID(roomID))
	} else {
		roomID = mat.RoomID(joinedRooms.JoinedRooms[roomIdx])
		fmt.Println("Selected room is", roomID)
		for userID, _ := range store.rooms[mat.RoomID(roomID)].Users {
			if userID == store.me.ID {
				continue
			}
			theirUser.id = userID
			break
		}
	}

	room := store.rooms[roomID]

	// TMP
	room.Users[theirUser.id] = &User{
		id: theirUser.id,
	}

	//theirUserDevices, err := theirUser.Devices()
	//if err != nil {
	//	panic(err)
	//}

	//deviceKeysAlgorithms := map[string]map[string]string{string(theirUserDevices.ID): map[string]string{}}
	//keysToClaim := 0
	//for theirDeviceID, _ := range theirUserDevices.Devices {
	//	if store.sessionsID.GetOlmSessionID(room.id, theirUserDevices.ID, theirDeviceID) == "" {
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
	//					olmSession, err := store.me.Device.OlmAccount.NewOutboundSession(device.Curve25519,
	//						oneTimeKey)
	//					if err != nil {
	//						panic(err)
	//					}
	//					store.sessionsID.setOlmSessionID(room.id, theirUserDevices.ID, device.ID, SessionID(olmSession.ID()))
	//					store.db.StoreOlmSessionID(room.id, theirUserDevices.ID, device.ID, SessionID(olmSession.ID()))
	//					device.OlmSessions[SessionID(olmSession.ID())] = olmSession
	//					store.db.StoreOlmSession(theirUserDevices.ID, device.ID, olmSession)
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

	if room.EncryptionAlg() == olm.AlgorithmNone {
		err := room.SetMegolmEncryption()
		if err != nil {
			panic(err)
		}
	}

	text := fmt.Sprint("I'm encrypted :D ~ ", time.Now().Format("2006-01-02 15:04:05"))
	err = room.SendText(text)
	err = room.SendText(text)
	err = room.SendText(text)
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
		Filter(res, roomID)
	}
}

func Filter(res *mat.RespSync, myRoomID mat.RoomID) {
	for roomID, roomData := range res.Rooms.Join {
		if roomID != string(myRoomID) {
			continue
		}
		//fmt.Printf("\t%s\n", roomID)
		for _, ev := range roomData.Timeline.Events {
			ev.RoomID = roomID
			sender, body := parseEvent(&ev)
			fmt.Printf("> [%s] %s %s\n",
				time.Unix(ev.Timestamp/1000, 0).Format("2006-01-02 15:04"),
				sender, body)
		}
	}
	for roomID, _ := range res.Rooms.Invite {
		_, err := cli.JoinRoom(roomID, "", nil)
		if err != nil {
			fmt.Printf("Err Couldn't auto join room %s\n", roomID)
		} else {
			fmt.Printf("INFO Autojoined room %s\n", roomID)
		}
	}
	for _, ev := range res.ToDevice.Events {
		sender, body := parseSendToDeviceEvent(&ev)
		fmt.Printf("$$$ %s %s\n", sender, body)
	}
}

// TODO: Delete this
type Ciphertext struct {
	Type olm.MsgType `json:"type"`
	Body string      `json:"body"`
}

type OlmMsg struct {
	Algorithm  olm.Algorithm  `json:"algorithm" mapstructure:"algorithm"`
	SenderKey  olm.Curve25519 `json:"sender_key" mapstructure:"sender_key"`
	Ciphertext map[olm.Curve25519]struct {
		Type olm.MsgType `json:"type" mapstructure:"type"`
		Body string      `json:"body" mapstructure:"body"`
	} `json:"ciphertext" mapstructure:"ciphertext"`
}

type RoomKey struct {
	// OlmSenderKey is filled when this event is obtained through the
	// decryption of an m.room.encrypted event.
	OlmSenderKey olm.Curve25519
	Algorithm    olm.Algorithm `json:"algorithm" mapstructure:"algorithm"`
	RoomID       mat.RoomID    `json:"room_id" mapstructure:"room_id"`
	SessionID    olm.SessionID `json:"session_id" mapstructure:"session_id"`
	SessionKey   string        `json:"session_key" mapstructure:"session_key"`
}

type MegolmMsg struct {
	Algorithm  olm.Algorithm  `json:"algorithm" mapstructure:"algorithm"`
	Ciphertext string         `json:"ciphertext" mapstructure:"ciphertext"`
	DeviceID   mat.DeviceID   `json:"device_id" mapstructure:"device_id"`
	SenderKey  olm.Curve25519 `json:"sender_key" mapstructure:"sender_key"`
	SessionID  olm.SessionID  `json:"session_id" mapstructure:"session_id"`
}

func decryptOlmMsg(olmMsg *OlmMsg, sender mat.UserID, roomID mat.RoomID) (string, error) {
	if olmMsg.SenderKey == store.me.Device.Curve25519 {
		// TODO: Cache self encrypted olm messages so that they can be queried here
		return "", fmt.Errorf("Olm encrypted messages by myself not cached yet")
	}
	// NOTE: olm messages can be decrypted without the sender keys
	device, err := GetUserDevice(sender, olmMsg.SenderKey)
	if err != nil {
		return "", err
	}
	ciphertext, ok := olmMsg.Ciphertext[store.me.Device.Curve25519]
	if !ok {
		return "", fmt.Errorf("Message not encrypted for our Curve25519 key %s",
			store.me.Device.Curve25519)
	}
	var session *olm.Session
	sessionsID := store.sessionsID.getSessionsID(roomID, sender, olmMsg.SenderKey)
	if sessionsID == nil {
		// Is this a pre key message where the sender has started an olm session?
		if ciphertext.Type == olm.MsgTypePreKey {
			session, err = store.me.Device.OlmAccount.
				NewInboundSession(ciphertext.Body)
			if err != nil {
				return "", err
			}
			StoreNewOlmSession(roomID, sender, device, session)

		} else {
			return "", fmt.Errorf("No olm session stored for "+
				"room %s, user %s, device key %s", roomID, sender, olmMsg.SenderKey)
		}
	} else {
		session = device.OlmSessions[sessionsID.olmSessionID]
	}
	msg, err := session.Decrypt(ciphertext.Body, ciphertext.Type)
	if err != nil {
		// Is this a pre key message where the sender has started a new olm session?
		if ciphertext.Type == olm.MsgTypePreKey {
			session2, err2 := store.me.Device.OlmAccount.
				NewInboundSession(ciphertext.Body)
			if err2 != nil {
				return "", err
			}
			msg, err2 = session2.Decrypt(ciphertext.Body, ciphertext.Type)
			if err2 != nil {
				return "", err
			}
			session = session2
			StoreNewOlmSession(roomID, sender, device, session)
			return msg, nil
		} else {
			return "", err
		}
	}
	store.db.StoreOlmSession(sender, device.ID, session)
	return msg, nil
}

func decryptMegolmMsg(megolmMsg *MegolmMsg, sender mat.UserID, roomID mat.RoomID) (string, error) {
	if megolmMsg.SenderKey == store.me.Device.Curve25519 {
		// TODO: Figure this out
		return "", fmt.Errorf("Megolm encrypted message by myself")
	}
	device, err := GetUserDevice(sender, megolmMsg.SenderKey)
	if err != nil {
		return "", err
	}
	ciphertext := megolmMsg.Ciphertext
	var session *olm.InboundGroupSession
	sessionsID := store.sessionsID.getSessionsID(roomID, sender, megolmMsg.SenderKey)
	if sessionsID == nil {
		// TODO: (UserID, SenderKey) hasn't sent their megolm session
		// key, request it TODO: After sending the request we may not
		// get the session key immediately, figure out a way to notify
		// the client that the messages can now be decrypted upong
		// receiving such key
		return "", fmt.Errorf("User %s with device key %s hasn't sent us the megolm"+
			"session key", sender, megolmMsg.SenderKey)
	}
	session = device.MegolmInSessions[megolmMsg.SessionID]
	msg, _, err := session.Decrypt(ciphertext)
	if err != nil {
		// TODO: Depending on the error type, we may decide to request they key
		return "", fmt.Errorf("Unable to decrypt the megolm encrypted message: %s", err)
	}
	store.db.StoreMegolmInSession(sender, device.ID, session)
	return msg, nil
}

func parseEvent(ev *mat.Event) (sender string, body string) {
	sender = fmt.Sprintf("%s:", ev.Sender)
	userID := mat.UserID(ev.Sender)
	roomID := mat.RoomID(ev.RoomID)
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
	case "m.room.encrypted":
		sender, body = parseRoomEncrypted(roomID, userID, ev.Content)
	case "m.room_key":
		sender, body = parseRoomKey(roomID, userID, ev.Content)
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

func parseSendToDeviceEvent(_ev *mat.SendToDeviceEvent) (sender string, body string) {
	ev := &mat.Event{Sender: _ev.Sender, Type: _ev.Type, Content: _ev.Content}
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

func parseRoomEncrypted(roomID mat.RoomID, userID mat.UserID,
	content map[string]interface{}) (sender string, body string) {
	sender = fmt.Sprintf("%s:", userID)

	var decEventJSON string
	var decEvent mat.Event
	var senderKey olm.Curve25519
	var err error
	switch content["algorithm"] {
	case string(olm.AlgorithmOlmV1):
		var olmMsg OlmMsg
		err = mapstructure.Decode(content, &olmMsg)
		if err != nil {
			break
		}
		senderKey = olmMsg.SenderKey
		decEventJSON, err = decryptOlmMsg(&olmMsg, userID, roomID)
	case string(olm.AlgorithmMegolmV1):
		var megolmMsg MegolmMsg
		err = mapstructure.Decode(content, &megolmMsg)
		if err != nil {
			break
		}
		senderKey = megolmMsg.SenderKey
		decEventJSON, err = decryptMegolmMsg(&megolmMsg, userID, roomID)
	default:
		err = fmt.Errorf("Encryption algorithm %s not supported", content["algorithm"])
	}
	if err == nil {
		err = json.Unmarshal([]byte(decEventJSON), &decEvent)
	}
	if err != nil {
		body = fmt.Sprintf("ERROR - Unable to decrypt: %s", err)
	} else {
		switch decEvent.Type {
		case "m.room_key_request":
			fallthrough
		case "m.forwarded_room_key":
			fallthrough
		case "m.room_key":
			decEvent.Content["OlmSenderKey"] = senderKey
		}
		sender, body = parseEvent(&decEvent)
	}
	sender = fmt.Sprintf("[E] %s", sender)
	return
}

func parseRoomKey(roomID mat.RoomID, userID mat.UserID,
	content map[string]interface{}) (sender string, body string) {
	var roomKey RoomKey
	err := mapstructure.Decode(content, &roomKey)
	if err != nil {
		body = fmt.Sprintf("ERROR - Parsing m.room_key event: %s", err)
		return
	}
	senderKey := roomKey.OlmSenderKey
	switch roomKey.Algorithm {
	case olm.AlgorithmMegolmV1:
		sessionsID := store.sessionsID.getSessionsID(roomKey.RoomID, userID, senderKey)
		if sessionsID == nil {
			sessionsID = store.sessionsID.makeSessionsID(roomKey.RoomID,
				userID, senderKey)
		}
		switch sessionsID.megolmInSessionID {
		case roomKey.SessionID:
			err = fmt.Errorf("Megolm session key for session id %s already exists",
				roomKey.SessionID)
		case "":
			fallthrough
		default:
			// DEBUG: Replacing Megolm session key for (room, user, device)
		}
		if err != nil {
			break
		}
		device, err := GetUserDevice(userID, senderKey)
		if err != nil {
			break
		}
		session, err := olm.NewInboundGroupSession([]byte(roomKey.SessionKey))
		if err != nil {
			break
		}
		store.sessionsID.setMegolmSessionID(roomKey.RoomID, userID,
			senderKey, roomKey.SessionID)
		store.db.StoreMegolmInSessionID(roomID, userID, device.Curve25519, session.ID())
		device.MegolmInSessions[session.ID()] = session
		store.db.StoreMegolmInSession(userID, device.ID, session)
	default:
		body = fmt.Sprintf("Unhandled room_key.algorithm %s", roomKey.Algorithm)
	}
	if err != nil {
		body = fmt.Sprintf("ERROR - Parsing m.room_key event: %s", err)
	}
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
