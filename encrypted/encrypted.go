package main

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"github.com/boltdb/bolt"
	mat "github.com/matrix-org/gomatrix"
	"github.com/mitchellh/mapstructure"
	olm "gitlab.com/dhole/go-olm"
	"log"
	"os"
	"strconv"
	"strings"
	"time"
)

var userID = mat.UserID("@ray_test:matrix.org")
var username = "ray_test"
var homeserver = "https://matrix.org"
var password = ""
var deviceID = mat.DeviceID("5un3HpnWE")
var deviceDisplayName = "go-olm-dev"

type EncryptionAlg string

const (
	EncryptionAlgNone   EncryptionAlg = ""
	EncryptionAlgOlm    EncryptionAlg = "m.olm.v1.curve25519-aes-sha2"
	EncryptionAlgMegolm EncryptionAlg = "m.megolm.v1.aes-sha2"
)

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
	db         *CryptoDB
}

// LoadStore loads the Store datastructure.  It sets up the database and loads
// its contents into the rest of the datastructure.
func LoadStore(userID mat.UserID, deviceID mat.DeviceID, dbPath string) (*Store, error) {
	var store Store

	// Load/Create database
	db, err := OpenCryptoDB(dbPath)
	if err != nil {
		return nil, err
	} else {
		store.db = db
	}

	// Load/Create myUser olm data
	store.db.AddMyUserMyDevice(userID, deviceID)
	if !store.db.ExistsOlmAccount(userID, deviceID) {
		olmAccount := olm.NewAccount()
		store.db.StoreOlmAccount(userID, deviceID,
			olmAccount)
		// TODO: Upload device identity keys to server using `/keys/upload`
		// TODO: Upload device one-time keys to server using `/keys/upload`
	}
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
	Ed25519    string // SigningKey
	Curve25519 string // IdentityKey
	//OneTimeKey       string                              // IdentityKey
	OlmSessions      map[mat.SessionID]*olm.Session
	MegolmInSessions map[mat.SessionID]*olm.InboundGroupSession
}

func NewDevice(deviceID mat.DeviceID) *Device {
	return &Device{
		ID:               mat.DeviceID(deviceID),
		OlmSessions:      make(map[mat.SessionID]*olm.Session),
		MegolmInSessions: make(map[mat.SessionID]*olm.InboundGroupSession),
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

	var oneTimeKey string
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
			var OTK SignedKey
			err := mapstructure.Decode(rawOTK, &OTK)
			if err != nil {
				return nil, err
			}

			oneTimeKey = OTK.Key
			olmSession, err := store.me.Device.OlmAccount.NewOutboundSession(
				d.Curve25519, oneTimeKey)
			if err != nil {
				return nil, err
			}
			store.sessionsID.setOlmSessionID(roomID, userID, d.Curve25519,
				mat.SessionID(olmSession.ID()))
			store.db.StoreOlmSessioID(roomID, userID, d.Curve25519, mat.SessionID(olmSession.ID()))
			d.OlmSessions[mat.SessionID(olmSession.ID())] = olmSession
			store.db.StoreOlmSession(userID, d.ID, olmSession)

			return olmSession, nil
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
	olmSession, ok := d.OlmSessions[store.sessionsID.GetOlmSessionID(roomID, userID, d.Curve25519)]
	if !ok {
		var err error
		olmSession, err = d.NewOlmSession(roomID, userID)
		if err != nil {
			return 0, "", err
		}
	}
	payload := map[string]interface{}{
		"type":           eventType,
		"content":        contentJSON,
		"recipient":      userID,
		"sender":         store.me.ID,
		"recipient_keys": map[string]string{"ed25519": d.Ed25519},
		"room_id":        roomID}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		panic(err)
	}
	//fmt.Println(string(payloadJSON))
	encryptMsgType, encryptedMsg := olmSession.Encrypt(string(payloadJSON))
	store.db.StoreOlmSession(userID, d.ID, olmSession)

	return encryptMsgType, encryptedMsg, nil
}

type MyDevice struct {
	ID         mat.DeviceID
	Ed25519    string // Ed25519
	Curve25519 string // IdentityKey
	OlmAccount *olm.Account
	//OlmSessions       map[string]*olm.Session              // key:room_id
	MegolmOutSessions map[mat.RoomID]*olm.OutboundGroupSession
}

type UserDevices struct {
	ID                mat.UserID
	Devices           map[string]*Device // Devices by Curve25519 key
	DevicesByID       map[mat.DeviceID]*Device
	devicesTracking   bool
	devicesOutdated   bool
	devicesLastUpdate int64
}

func NewUserDevices(userID mat.UserID) *UserDevices {
	return &UserDevices{
		ID:          userID,
		Devices:     make(map[string]*Device),
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
				device.Ed25519 = key
			case "curve25519":
				device.Curve25519 = key
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
			fmt.Sprintf("curve25519:%s", me.Device.ID): me.Device.Curve25519,
			fmt.Sprintf("ed25519:%s", me.Device.ID):    me.Device.Ed25519,
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
		oneTimeKeys[keyID] = otk
	}
	// olmAccount.MarkKeysAsPublished()
	fmt.Printf("\n%+v\n%+v\n", deviceKeys, oneTimeKeys)
	return nil
}

type SessionsID struct {
	olmSessionID      mat.SessionID
	megolmInSessionID mat.SessionID
}

type Room struct {
	id    mat.RoomID
	name  string
	Users map[mat.UserID]*User
	// TODO: encryption type
	encryptionAlg EncryptionAlg
	//MegolmOutSession *olm.OutboundGroupSession
}

func NewRoom(roomID mat.RoomID) *Room {
	return &Room{
		id:    roomID,
		Users: make(map[mat.UserID]*User),
	}
}

func (r *Room) EncryptionAlg() EncryptionAlg {
	return r.encryptionAlg
}

func (r *Room) SetOlmEncryption() error {
	return r.SetEncryption(EncryptionAlgOlm)
}

func (r *Room) SetMegolmEncryption() error {
	return r.SetEncryption(EncryptionAlgMegolm)
}

func (r *Room) SetEncryption(encryptionAlg EncryptionAlg) error {
	if r.encryptionAlg == EncryptionAlgNone {
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
	case EncryptionAlgNone:
		return r.sendPlaintextMsg(eventType, contentJSON)
	case EncryptionAlgOlm:
		return r.sendOlmMsg(eventType, contentJSON)
	case EncryptionAlgMegolm:
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

// TODO: It seems I can batch all the encrypted messages into one, identified
// by the Curve25519 key of the device of the user.
func (r *Room) sendOlmMsg(eventType string, contentJSON interface{}) SendEncEventErrors {
	var errs SendEncEventErrors
	ciphertext := make(map[string]Ciphertext)
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
		"algorithm":  "m.olm.v1.curve25519-aes-sha2",
		"ciphertext": ciphertext,
		"sender_key": store.me.Device.Curve25519}
	_, err := cli.SendMessageEvent(string(r.id), "m.room.encrypted", contentJSONEnc)
	if err != nil {
		errs = append(errs, SendEncEventError{Err: err})
	}
	return errs
}

// TODO
func (r *Room) sendMegolmMsg(eventType string, contentJSON interface{}) SendEncEventErrors {
	return SendEncEventErrors([]SendEncEventError{SendEncEventError{Err: fmt.Errorf("Not implemented yet")}})
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
func (u *User) Devices() (*UserDevices, error) {
	userDevices, ok := store.users[u.id]
	if ok {
		return userDevices, nil
	} else {
		var userDevices *UserDevices
		userDevices = NewUserDevices(u.id)
		store.users[userDevices.ID] = userDevices
		store.db.AddUser(userDevices.ID)

		if err := userDevices.Update(); err != nil {
			return nil, err
		}
		return userDevices, nil
	}
}

// RoomsSessionsID maps (RoomID, UserID, Curve25519) to (olmSessionID, megolmInSessionID)
type RoomsSessionsID struct {
	roomIDuserIDKey map[mat.RoomID]map[mat.UserID]map[string]*SessionsID
}

func (rs *RoomsSessionsID) getSessionsID(roomID mat.RoomID, userID mat.UserID,
	key string) *SessionsID { // Where key is Curve25519
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
	key string) *SessionsID { // Where key is Curve25519
	room, ok := rs.roomIDuserIDKey[roomID]
	if !ok {
		rs.roomIDuserIDKey[roomID] = make(map[mat.UserID]map[string]*SessionsID)
		room = rs.roomIDuserIDKey[roomID]
	}
	user, ok := room[userID]
	if !ok {
		room[userID] = make(map[string]*SessionsID)
		user = room[userID]
	}
	sessionsID, ok := user[key]
	if !ok {
		user[key] = &SessionsID{}
		sessionsID = user[key]
	}
	return sessionsID
}

func (rs *RoomsSessionsID) GetOlmSessionID(roomID mat.RoomID, userID mat.UserID, key string) mat.SessionID {
	sessionsID := rs.getSessionsID(roomID, userID, key)
	if sessionsID == nil {
		return ""
	} else {
		return sessionsID.olmSessionID
	}
}

func (rs *RoomsSessionsID) setOlmSessionID(roomID mat.RoomID, userID mat.UserID, key string,
	sessionID mat.SessionID) {
	sessionsID := rs.makeSessionsID(roomID, userID, key)
	sessionsID.olmSessionID = sessionID
}

func (rs *RoomsSessionsID) setMegolmSessionID(roomID mat.RoomID, userID mat.UserID, key string,
	sessionID mat.SessionID) {
	sessionsID := rs.makeSessionsID(roomID, userID, key)
	sessionsID.megolmInSessionID = sessionID
}

type SignedKey struct {
	Key        string                       `json:"key"`
	Signatures map[string]map[string]string `json:"signatures"`
}

type CryptoDB struct {
	db *bolt.DB
}

// OpenCryptoDB opens the DB and initializes the /crypto bucket if necessary
func OpenCryptoDB(filename string) (*CryptoDB, error) {
	var cdb CryptoDB
	db, err := bolt.Open(filename, 0660, &bolt.Options{Timeout: 200 * time.Millisecond})
	cdb.db = db
	if err != nil {
		return nil, err
	}
	// Create base buckets
	err = cdb.db.Update(func(tx *bolt.Tx) error {
		for _, bucket := range []string{
			"crypto_me", "crypto_users", "crypto_sessions_id", "crypto_rooms"} {
			_, err := tx.CreateBucketIfNotExists([]byte(bucket))
			if err != nil {
				return fmt.Errorf("create bucket: %s", err)
			}
		}
		return nil
	})
	return &cdb, err
}

// Close closes the DB
func (cdb *CryptoDB) Close() {
	cdb.db.Close()
}

// ExistsUser checks if /crypto_users/<userID>/ exists
//func (cdb *CryptoDB) ExistsUser(userID UserID) bool {
//	userExists := false
//	cdb.db.View(func(tx *bolt.Tx) error {
//		cryptoUsersBucket := tx.Bucket([]byte("crypto_users"))
//		userBucket := cryptoUsersBucket.Bucket([]byte(userID))
//		if userBucket == nil {
//			return nil
//		}
//		userExists = true
//		return nil
//	})
//	return userExists
//}

// AddUser adds /crypto_users/<userID>/ bucket
func (cdb *CryptoDB) AddUser(userID mat.UserID) error {
	err := cdb.db.Update(func(tx *bolt.Tx) error {
		cryptoUsersBucket := tx.Bucket([]byte("crypto_users"))
		userBucket, err := cryptoUsersBucket.CreateBucketIfNotExists([]byte(userID))
		if err != nil {
			return fmt.Errorf("create bucket: %s", err)
		}
		_, err = userBucket.CreateBucketIfNotExists([]byte("devices"))
		if err != nil {
			return fmt.Errorf("create bucket: %s", err)
		}
		userBucket.Put([]byte("devices_tracking"), bool2bytes(false))
		userBucket.Put([]byte("devices_outdated"), bool2bytes(false))
		userBucket.Put([]byte("devices_last_update"), int64tobytes(0))
		return nil
	})
	return err
}

// ExistsUserDevice checks if /crypto_users/<userID>/<deviceID>/ exists
//func (cdb *CryptoDB) ExistsUserDevice(userID UserID, deviceID DeviceID) bool {
//	deviceExists := false
//	cdb.db.View(func(tx *bolt.Tx) error {
//		cryptoUsersBucket := tx.Bucket([]byte("crypto_users"))
//		userBucket := cryptoUsersBucket.Bucket([]byte(userID))
//		if userBucket == nil {
//			return nil
//		}
//		deviceBucket := userBucket.Bucket([]byte(deviceID))
//		if deviceBucket == nil {
//			return nil
//		}
//		deviceExists = true
//		return nil
//	})
//	return deviceExists
//}

// AddUserDevice adds /crypto_users/<userID>/devices/<deviceID>/{olm,megolm_in}/ buckets
func (cdb *CryptoDB) AddUserDevice(userID mat.UserID, deviceID mat.DeviceID) error {
	err := cdb.db.Update(func(tx *bolt.Tx) error {
		cryptoUsersBucket := tx.Bucket([]byte("crypto_users"))
		userBucket, err := cryptoUsersBucket.CreateBucketIfNotExists([]byte(userID))
		if err != nil {
			return fmt.Errorf("create bucket: %s", err)
		}
		devicesBucket, err := userBucket.CreateBucketIfNotExists([]byte("devices"))
		if err != nil {
			return fmt.Errorf("create bucket: %s", err)
		}
		deviceBucket, err := devicesBucket.CreateBucketIfNotExists([]byte(deviceID))
		if err != nil {
			return fmt.Errorf("create bucket: %s", err)
		}
		_, err = deviceBucket.CreateBucketIfNotExists([]byte("olm"))
		if err != nil {
			return fmt.Errorf("create bucket: %s", err)
		}
		_, err = deviceBucket.CreateBucketIfNotExists([]byte("megolm_in"))
		if err != nil {
			return fmt.Errorf("create bucket: %s", err)
		}
		return nil
	})
	return err
}

// AddMyUserMyDevice adds /crypto_me/<userID>/<deviceID>/megolm_out/ buckets
func (cdb *CryptoDB) AddMyUserMyDevice(userID mat.UserID, deviceID mat.DeviceID) error {
	err := cdb.db.Update(func(tx *bolt.Tx) error {
		cryptoUsersBucket := tx.Bucket([]byte("crypto_me"))
		userBucket, err := cryptoUsersBucket.CreateBucketIfNotExists([]byte(userID))
		if err != nil {
			return fmt.Errorf("create bucket: %s", err)
		}
		deviceBucket, err := userBucket.CreateBucketIfNotExists([]byte(deviceID))
		if err != nil {
			return fmt.Errorf("create bucket: %s", err)
		}
		_, err = deviceBucket.CreateBucketIfNotExists([]byte("megolm_out"))
		if err != nil {
			return fmt.Errorf("create bucket: %s", err)
		}
		return nil
	})
	return err
}

// ExistsPubKeys checks if the ed25519 and curve25519 public keys exist at
// /crypto_users/<userID>/<deviceID>/
//func (cdb *CryptoDB) ExistsPubKeys(userID UserID, deviceID DeviceID) bool {
//	pubKeysExist := false
//	cdb.db.View(func(tx *bolt.Tx) error {
//		deviceBucket := tx.Bucket([]byte("crypto_users")).Bucket([]byte(userID)).
//			Bucket([]byte(deviceID))
//		ed25519 := deviceBucket.Get([]byte("ed25519"))
//		curve25519 := deviceBucket.Get([]byte("curve25519"))
//		if ed25519 != nil && curve25519 != nil {
//			pubKeysExist = true
//		}
//		return nil
//	})
//	return pubKeysExist
//}

// StorePubKeys stores the ed25519 and curve25519 public keys at /crypto_users/<userID>/devices/<deviceID>/
func (cdb *CryptoDB) StorePubKeys(userID mat.UserID, deviceID mat.DeviceID,
	ed25519, curve25519 string) error {
	err := cdb.db.Update(func(tx *bolt.Tx) error {
		deviceBucket := tx.Bucket([]byte("crypto_users")).Bucket([]byte(userID)).
			Bucket([]byte("devices")).Bucket([]byte(deviceID))
		deviceBucket.Put([]byte("ed25519"), []byte(ed25519))
		deviceBucket.Put([]byte("curve25519"), []byte(curve25519))
		return nil
	})
	return err
}

// StoreOlmSession stores an olm.Session at /crypto_users/<userID>/devices/<deviceID>/olm/<olmSession.ID>
func (cdb *CryptoDB) StoreOlmSession(userID mat.UserID, deviceID mat.DeviceID,
	olmSession *olm.Session) error {
	err := cdb.db.Update(func(tx *bolt.Tx) error {
		olmSessionsBucket := tx.Bucket([]byte("crypto_users")).Bucket([]byte(userID)).
			Bucket([]byte("devices")).Bucket([]byte(deviceID)).Bucket([]byte("olm"))
		olmSessionsBucket.Put([]byte(olmSession.ID()), []byte(olmSession.Pickle([]byte(""))))
		return nil
	})
	return err
}

// StoreMegolmInSession stores an olm.InboundGroupSession at
// /crypto_users/<userID>/devices/<deviceID>/megolm_in/<megolmInSession.ID>
func (cdb *CryptoDB) StoreMegolmInSession(userID mat.UserID, deviceID mat.DeviceID,
	megolmInSession *olm.InboundGroupSession) error {
	err := cdb.db.Update(func(tx *bolt.Tx) error {
		megolmInSessionsBucket := tx.Bucket([]byte("crypto_users")).Bucket([]byte(userID)).
			Bucket([]byte(deviceID)).Bucket([]byte("megolm_in"))
		megolmInSessionsBucket.Put([]byte(megolmInSession.ID()),
			[]byte(megolmInSession.Pickle([]byte(""))))
		return nil
	})
	return err
}

// StoreMegolmOutSession stores an olm.OutboundGroupSession at
// /crypto_me/<userID>/devices/<deviceID>/megolm_out/<megolmOutSession.ID>
func (cdb *CryptoDB) StoreMegolmOutSession(userID mat.UserID, deviceID mat.DeviceID,
	megolmOutSession *olm.OutboundGroupSession) error {
	err := cdb.db.Update(func(tx *bolt.Tx) error {
		megolmOutSessionsBucket := tx.Bucket([]byte("crypto_me")).Bucket([]byte(userID)).
			Bucket([]byte("devices")).Bucket([]byte(deviceID)).Bucket([]byte("megolm_out"))
		megolmOutSessionsBucket.Put([]byte(megolmOutSession.ID()),
			[]byte(megolmOutSession.Pickle([]byte(""))))
		return nil
	})
	return err
}

func deviceFromBucket(deviceID mat.DeviceID, deviceBucket *bolt.Bucket) (*Device, error) {
	device := &Device{
		ID:               deviceID,
		Ed25519:          string(deviceBucket.Get([]byte("ed25519"))),
		Curve25519:       string(deviceBucket.Get([]byte("curve25519"))),
		OlmSessions:      make(map[mat.SessionID]*olm.Session),
		MegolmInSessions: make(map[mat.SessionID]*olm.InboundGroupSession),
	}
	olmSessionsBucket := deviceBucket.Bucket([]byte("olm"))
	err := olmSessionsBucket.ForEach(func(sessionID, session []byte) error {
		var err error
		device.OlmSessions[mat.SessionID(sessionID)], err =
			olm.SessionFromPickled(string(session), []byte(""))
		return err
	})
	if err != nil {
		return nil, err
	}
	megolmInSessionsBucket := deviceBucket.Bucket([]byte("megolm_in"))
	err = megolmInSessionsBucket.ForEach(func(sessionID, session []byte) error {
		var err error
		device.MegolmInSessions[mat.SessionID(sessionID)], err =
			olm.InboundGroupSessionFromPickled(string(session), []byte(""))
		return err
	})
	return device, err
}

func bool2bytes(v bool) []byte {
	if v {
		return []byte{1}
	} else {
		return []byte{0}
	}
}

func bytes2bool(b []byte) bool {
	if b[0] == 0 {
		return false
	} else {
		return true
	}
}

func int64tobytes(v int64) []byte {
	b := make([]byte, 8)
	binary.LittleEndian.PutUint64(b, uint64(v))
	return b
}

func bytes2int64(b []byte) int64 {
	return int64(binary.LittleEndian.Uint64(b))
}

func userDevicesFromBucket(userID mat.UserID, userBucket *bolt.Bucket) (*UserDevices, error) {
	user := NewUserDevices(userID)
	var err error
	user.devicesTracking = bytes2bool(userBucket.Get([]byte("devices_tracking")))
	user.devicesOutdated = bytes2bool(userBucket.Get([]byte("devices_outdated")))
	user.devicesLastUpdate = bytes2int64(userBucket.Get([]byte("devices_last_update")))
	devicesBucket := userBucket.Bucket([]byte("devices"))
	err = devicesBucket.ForEach(func(deviceID, v []byte) error {
		deviceBucket := devicesBucket.Bucket(deviceID)
		device, err := deviceFromBucket(mat.DeviceID(deviceID), deviceBucket)
		user.Devices[device.Curve25519] = device
		user.DevicesByID[device.ID] = device
		return err
	})
	return user, err
}

// LoadSingleUserDevices loads the UserDevices at /crypto_users/<userID>/
//func (cdb *CryptoDB) LoadSingleUserDevices(userID UserID) (*UserDevices, error) {
//	user := &UserDevices{
//		ID:      userID,
//		Devices: make(map[DeviceID]*Device),
//	}
//	fmt.Println(">>> ", userID)
//	err := cdb.db.View(func(tx *bolt.Tx) error {
//		userBucket := tx.Bucket([]byte("crypto_users")).Bucket([]byte(userID))
//		err := userBucket.ForEach(func(deviceID, v []byte) error {
//			deviceBucket := userBucket.Bucket(deviceID)
//			device, err := deviceFromBucket(DeviceID(deviceID), deviceBucket)
//			user.Devices[DeviceID(deviceID)] = device
//			return err
//		})
//		return err
//	})
//	return user, err
//}

// LoadAllUserDevices loads all the UserDevices from /crypto_users/
func (cdb *CryptoDB) LoadAllUserDevices() (map[mat.UserID]*UserDevices, error) {
	users := make(map[mat.UserID]*UserDevices)
	err := cdb.db.View(func(tx *bolt.Tx) error {
		cryptoUsersBucket := tx.Bucket([]byte("crypto_users"))
		err := cryptoUsersBucket.ForEach(func(userID, v []byte) error {
			userBucket := cryptoUsersBucket.Bucket([]byte(userID))
			user, err := userDevicesFromBucket(mat.UserID(userID), userBucket)
			users[mat.UserID(userID)] = user
			return err
		})
		return err
	})
	return users, err
}

func myDeviceFromBucket(deviceID mat.DeviceID, deviceBucket *bolt.Bucket) (*MyDevice, error) {
	device := &MyDevice{
		ID:                mat.DeviceID(deviceID),
		Ed25519:           string(deviceBucket.Get([]byte("ed25519"))),
		Curve25519:        string(deviceBucket.Get([]byte("curve25519"))),
		MegolmOutSessions: make(map[mat.RoomID]*olm.OutboundGroupSession),
	}
	var err error
	device.OlmAccount, err = olm.AccountFromPickled(
		string(deviceBucket.Get([]byte("account"))), []byte(""))
	if err != nil {
		return nil, err
	}
	megolmOutSessionsBucket := deviceBucket.Bucket([]byte("megolm_out"))
	err = megolmOutSessionsBucket.ForEach(func(roomID, session []byte) error {
		var err error
		device.MegolmOutSessions[mat.RoomID(roomID)], err =
			olm.OutboundGroupSessionFromPickled(string(session), []byte(""))
		return err
	})
	return device, err
}

// LoadMyUserDevice loads the MyUserDevice at /crypto_me/<userID>/
func (cdb *CryptoDB) LoadMyUserDevice(userID mat.UserID) (*MyUserDevice, error) {
	myUser := &MyUserDevice{
		ID: userID,
	}
	err := cdb.db.View(func(tx *bolt.Tx) error {
		userBucket := tx.Bucket([]byte("crypto_me")).Bucket([]byte(userID))
		// Load the first device in the bucket (there should only be one)
		err := userBucket.ForEach(func(deviceID, v []byte) error {
			deviceBucket := userBucket.Bucket(deviceID)
			device, err := myDeviceFromBucket(mat.DeviceID(deviceID), deviceBucket)
			myUser.Device = device
			return err
		})
		return err
	})
	return myUser, err
}

// ExistsOlmAccount checks if an olm.Account exists at /crypto_me/<userID>/<deviceID>/
func (cdb *CryptoDB) ExistsOlmAccount(userID mat.UserID, deviceID mat.DeviceID) bool {
	olmAccountExists := false
	cdb.db.View(func(tx *bolt.Tx) error {
		deviceBucket := tx.Bucket([]byte("crypto_me")).Bucket([]byte(userID)).
			Bucket([]byte(deviceID))
		ed25519 := deviceBucket.Get([]byte("ed25519"))
		curve25519 := deviceBucket.Get([]byte("curve25519"))
		account := deviceBucket.Get([]byte("account"))
		if ed25519 != nil && curve25519 != nil && account != nil {
			olmAccountExists = true
		}
		return nil
	})
	return olmAccountExists
}

// StoreOlmAccount stores an olm.Account at /crypto_me/<userID>/<deviceID>/
func (cdb *CryptoDB) StoreOlmAccount(userID mat.UserID, deviceID mat.DeviceID, olmAccount *olm.Account) error {
	ed25519, curve25519 := olmAccount.IdentityKeysEd25519Curve25519()

	err := cdb.db.Update(func(tx *bolt.Tx) error {
		deviceBucket := tx.Bucket([]byte("crypto_me")).Bucket([]byte(userID)).
			Bucket([]byte(deviceID))
		deviceBucket.Put([]byte("ed25519"), []byte(ed25519))
		deviceBucket.Put([]byte("curve25519"), []byte(curve25519))
		deviceBucket.Put([]byte("account"), []byte(olmAccount.Pickle([]byte(""))))
		return nil
	})
	return err
}

// LoadOlmAccount loads the olm.Account at /crypto_me/<userID>/<deviceID>/
//func (cdb *CryptoDB) LoadOlmAccount(userID UserID, deviceID DeviceID) (*olm.Account, error) {
//	identityKeys := map[string]string{}
//	var pickledAccount string
//	err := cdb.db.View(func(tx *bolt.Tx) error {
//		deviceBucket := tx.Bucket([]byte("crypto_me")).Bucket([]byte(userID)).
//			Bucket([]byte(deviceID))
//		identityKeys["ed25519"] = string(deviceBucket.Get([]byte("ed25519")))
//		identityKeys["curve25519"] = string(deviceBucket.Get([]byte("curve25519")))
//		pickledAccount = string(deviceBucket.Get([]byte("account")))
//		return nil
//	})
//	if err != nil {
//		return nil, err
//	}
//	olmAccount, err := olm.AccountFromPickled(pickledAccount, []byte(""))
//	return olmAccount, err
//}

// StoreOlmSessioID stores an olm.Account at
// /crypto_sessions_id/<roomID>/<userID>/<deviceID>/olm_session_id
func (cdb *CryptoDB) StoreOlmSessioID(roomID mat.RoomID, userID mat.UserID, key string,
	sessionID mat.SessionID) error {
	err := cdb.db.Update(func(tx *bolt.Tx) error {
		cryptoRoomsBucket := tx.Bucket([]byte("crypto_sessions_id"))
		roomBucket, err := cryptoRoomsBucket.CreateBucketIfNotExists([]byte(roomID))
		if err != nil {
			return fmt.Errorf("create bucket: %s", err)
		}
		userBucket, err := roomBucket.CreateBucketIfNotExists([]byte(userID))
		if err != nil {
			return fmt.Errorf("create bucket: %s", err)
		}
		deviceBucket, err := userBucket.CreateBucketIfNotExists([]byte(key))
		if err != nil {
			return fmt.Errorf("create bucket: %s", err)
		}
		deviceBucket.Put([]byte("olm_session_id"), []byte(sessionID))
		return nil
	})
	return err
}

func updateMapSessionsIDFromBucket(roomID mat.RoomID, userID mat.UserID, key string,
	deviceBucket *bolt.Bucket, sessionsID *RoomsSessionsID) {
	olmSessionID := deviceBucket.Get([]byte("olm_session_id"))
	if olmSessionID != nil {
		sessionsID.setOlmSessionID(roomID, userID, key, mat.SessionID(olmSessionID))
	}
	megolmSessionID := deviceBucket.Get([]byte("megolm_session_id"))
	if megolmSessionID != nil {
		sessionsID.setMegolmSessionID(roomID, userID, key, mat.SessionID(megolmSessionID))
	}
}

// LoadMapSessionsID loads all the RoomsSessionsID at /crypto_sessions_id/
func (cdb *CryptoDB) LoadMapSessionsID() (*RoomsSessionsID, error) {
	sessionsID := RoomsSessionsID{
		roomIDuserIDKey: make(map[mat.RoomID]map[mat.UserID]map[string]*SessionsID),
	}
	err := cdb.db.View(func(tx *bolt.Tx) error {
		cryptoRoomsBucket := tx.Bucket([]byte("crypto_sessions_id"))
		err := cryptoRoomsBucket.ForEach(func(roomID, v []byte) error {
			roomBucket := cryptoRoomsBucket.Bucket([]byte(roomID))
			err := roomBucket.ForEach(func(userID, v []byte) error {
				userBucket := roomBucket.Bucket(userID)
				err := userBucket.ForEach(func(key, v []byte) error {
					deviceBucket := userBucket.Bucket(key)
					updateMapSessionsIDFromBucket(mat.RoomID(roomID),
						mat.UserID(userID), string(key),
						deviceBucket, &sessionsID)
					return nil
				})
				return err
			})
			return err
		})
		return err
	})
	return &sessionsID, err
}

func roomFromBucket(roomID mat.RoomID, roomBucket *bolt.Bucket) *Room {
	room := NewRoom(roomID)
	room.encryptionAlg = EncryptionAlg(roomBucket.Get([]byte("encryption_alg")))
	return room
}

func (cdb *CryptoDB) LoadRooms() (map[mat.RoomID]*Room, error) {
	rooms := make(map[mat.RoomID]*Room)
	err := cdb.db.View(func(tx *bolt.Tx) error {
		cryptoRoomsBucket := tx.Bucket([]byte("crypto_rooms"))
		err := cryptoRoomsBucket.ForEach(func(roomID, v []byte) error {
			roomBucket := cryptoRoomsBucket.Bucket([]byte(roomID))
			rooms[mat.RoomID(roomID)] = roomFromBucket(mat.RoomID(roomID), roomBucket)
			rooms[mat.RoomID(roomID)].Users = make(map[mat.UserID]*User)
			return nil
		})
		return err
	})
	return rooms, err
}

func (cdb *CryptoDB) StoreRoomEncryptionAlg(roomID mat.RoomID, encryptionAlg EncryptionAlg) error {
	err := cdb.db.View(func(tx *bolt.Tx) error {
		cryptoRoomsBucket := tx.Bucket([]byte("crypto_rooms"))
		roomBucket, err := cryptoRoomsBucket.CreateBucketIfNotExists([]byte(roomID))
		if err != nil {
			return fmt.Errorf("create bucket: %s", err)
		}
		roomBucket.Put([]byte("encryption_alg"), []byte(encryptionAlg))
		return nil
	})
	return err
}

func SplitAlgorithmKeyID(algorithmKeyID string) (string, string) {
	algorithmKeyIDSlice := strings.Split(algorithmKeyID, ":")
	if len(algorithmKeyIDSlice) != 2 {
		return "", ""
	}
	return algorithmKeyIDSlice[0], algorithmKeyIDSlice[1]
}

func main() {
	password = os.Args[1]

	var err error
	store, err = LoadStore(userID, deviceID, "test.db")
	if err != nil {
		store.Close()
		log.Fatal(err)
	}
	defer store.Close()

	err = store.me.KeysUpload()
	if err != nil {
		panic(err)
	}
	return

	cli, _ = mat.NewClient(homeserver, "", "")
	cli.Prefix = "/_matrix/client/unstable"
	fmt.Println("Logging in...")
	resLogin, err := cli.Login(&mat.ReqLogin{
		Type:                     "m.login.password",
		User:                     username,
		Password:                 password,
		DeviceID:                 string(deviceID),
		InitialDeviceDisplayName: deviceDisplayName,
	})
	if err != nil {
		panic(err)
	}
	cli.SetCredentials(resLogin.UserID, resLogin.AccessToken)

	joinedRooms, err := cli.JoinedRooms()
	if err != nil {
		panic(err)
	}
	for i := 0; i < 2; i++ {
		//	for i := 0; i < len(joinedRooms.JoinedRooms); i++ {
		roomID := mat.RoomID(joinedRooms.JoinedRooms[i])
		if store.rooms[roomID] == nil {
			room := NewRoom(roomID)
			room.encryptionAlg = EncryptionAlgNone
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
	fmt.Scanln(&input)
	roomIdx, err := strconv.Atoi(input)
	var roomID mat.RoomID
	var theirUser User
	if err != nil {
		fmt.Println("Creating new room...")
		fmt.Printf("Write user ID to invite: ")
		fmt.Scanln(&input)
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
	//					store.db.StoreOlmSessioID(room.id, theirUserDevices.ID, device.ID, SessionID(olmSession.ID()))
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

	if room.EncryptionAlg() != EncryptionAlgOlm {
		err = room.SetOlmEncryption()
		if err != nil {
			panic(err)
		}
	}

	text := fmt.Sprint("I'm encrypted :D ~ ", time.Now().Format("2006-01-02 15:04:05"))
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
		for _, ev := range roomData.Timeline.Events {
			ev.RoomID = roomID
			sender, body := parseEvent(&ev)
			fmt.Printf("> [%s] %s %s\n",
				time.Unix(ev.Timestamp/1000, 0).Format("2006-01-02 15:04"),
				sender, body)
		}
	}
}

type Ciphertext struct {
	Type olm.MsgType `json:"type"`
	Body string      `json:"body"`
}

func parseEvent(ev *mat.Event) (sender string, body string) {
	sender = fmt.Sprintf("%s:", ev.Sender)
	body = "???"
	if ev.Type == "m.room.message" {
		switch ev.Content["msgtype"] {
		case "m.text":
		case "m.emote":
			sender = fmt.Sprintf("* %s", ev.Sender)
		case "m.notice":
			sender = fmt.Sprintf("%s ~", ev.Sender)
		}
		body, _ = ev.Content["body"].(string)
	} else if ev.Type == "m.room.encrypted" {
		errMsg := ""
		switch ev.Content["algorithm"] {
		case "m.olm.v1.curve25519-aes-sha2":
			//fmt.Printf("$ %+v\n", ev)
			//sessionID := ev.Content["session_id"].(string)
			deviceKey := ev.Content["sender_key"].(string)
			userDevices := store.users[mat.UserID(ev.Sender)]
			if userDevices == nil {
				errMsg = fmt.Sprintf("User %s not stored", ev.Sender)
				break
			}
			device := userDevices.Devices[deviceKey]
			if device == nil {
				errMsg = fmt.Sprintf("Device with key %s for user %s not stored",
					deviceKey, ev.Sender)
				break
			}
			sessionsID := store.sessionsID.getSessionsID(mat.RoomID(ev.RoomID),
				mat.UserID(ev.Sender), deviceKey)
			if sessionsID == nil {
				errMsg = fmt.Sprintf("No olm session stored for room %s, user %s, device key %s",
					ev.RoomID, ev.Sender, deviceKey)
				break
			}
			sessionID := sessionsID.olmSessionID
			//if storedSessionID != SessionID(sessionID) {
			//	body = fmt.Sprintf("ERROR: %s",
			//		"Stored session_id doesn't match")
			//	break
			//}
			var ciphertext map[string]Ciphertext
			err := mapstructure.Decode(ev.Content["ciphertext"], &ciphertext)
			if err != nil {
				errMsg = "Error decoding ciphertext JSON field"
				break
			}
			ciphDev, ok := ciphertext[store.me.Device.Curve25519]
			if !ok {
				errMsg = fmt.Sprintf("Message not encrypted for our Curve25519 key %s",
					store.me.Device.Curve25519)
				break
			}
			encMsg := ciphDev.Body
			msgType := ciphDev.Type
			olmSession := device.OlmSessions[mat.SessionID(sessionID)]
			msg, err := olmSession.Decrypt(encMsg, msgType)
			store.db.StoreOlmSession(mat.UserID(ev.Sender), device.ID, olmSession)
			if err != nil {
				errMsg = err.Error()
				break
			}
			body = msg
		}
		if errMsg != "" {
			body = fmt.Sprintf("ERROR: %s", errMsg)
		}
		sender = fmt.Sprintf("[E] %s", sender)
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
