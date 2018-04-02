package main

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"github.com/boltdb/bolt"
	"github.com/matrix-org/gomatrix"
	"github.com/mitchellh/mapstructure"
	olm "gitlab.com/dhole/go-olm"
	"log"
	"os"
	"strconv"
	"strings"
	"time"
)

var userID = UserID("@ray_test:matrix.org")
var username = "ray_test"
var homeserver = "https://matrix.org"
var password = ""
var deviceID = DeviceID("5un3HpnWE")
var deviceDisplayName = "go-olm-dev"

type EncryptionAlg string

const (
	EncryptionAlgNone   EncryptionAlg = ""
	EncryptionAlgOlm    EncryptionAlg = "m.olm.v1.curve25519-aes-sha2"
	EncryptionAlgMegolm EncryptionAlg = "m.megolm.v1.aes-sha2"
)

type RoomID string
type UserID string
type SessionID string
type DeviceID string

var store *Store

var cli *gomatrix.Client

// Store contains data structures that are completely or partly permanent
// (backed by a data base).
type Store struct {
	me         *MyUserDevice
	users      map[UserID]*UserDevices
	sessionsID *RoomsSessionsID
	rooms      map[RoomID]*Room
	db         *CryptoDB
}

// LoadStore loads the Store datastructure.  It sets up the database and loads
// its contents into the rest of the datastructure.
func LoadStore(userID UserID, deviceID DeviceID, dbPath string) (*Store, error) {
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

type Device struct {
	ID         DeviceID
	Ed25519    string // SigningKey
	Curve25519 string // IdentityKey
	//OneTimeKey       string                              // IdentityKey
	OlmSessions      map[SessionID]*olm.Session
	MegolmInSessions map[SessionID]*olm.InboundGroupSession
}

func NewDevice(deviceID DeviceID) *Device {
	return &Device{
		ID:               DeviceID(deviceID),
		OlmSessions:      make(map[SessionID]*olm.Session),
		MegolmInSessions: make(map[SessionID]*olm.InboundGroupSession),
	}
}

func (d *Device) EncryptOlmMsg(roomID RoomID, userID UserID, eventType string,
	contentJSON interface{}) interface{} {
	olmSession, ok := d.OlmSessions[store.sessionsID.GetOlmSessionID(roomID, userID, d.ID)]
	if !ok {
		// TODO: Create new olm session
		log.Fatal("TODO: Create new olm session")
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
	fmt.Println(string(payloadJSON))
	encryptMsgType, encryptedMsg := olmSession.Encrypt(string(payloadJSON))
	store.db.StoreOlmSession(userID, d.ID, olmSession)

	return map[string]interface{}{
		"algorithm": "m.olm.v1.curve25519-aes-sha2",
		"ciphertext": map[string]map[string]interface{}{
			d.Curve25519: map[string]interface{}{
				"type": encryptMsgType,
				"body": encryptedMsg,
			},
		},
		"device_id":  store.me.ID,
		"sender_key": store.me.Device.Curve25519,
		"session_id": olmSession.ID()}
}

type MyDevice struct {
	ID         DeviceID
	Ed25519    string // Ed25519
	Curve25519 string // IdentityKey
	OlmAccount *olm.Account
	//OlmSessions       map[string]*olm.Session              // key:room_id
	MegolmOutSessions map[RoomID]*olm.OutboundGroupSession
}

type UserDevices struct {
	ID      UserID
	Devices map[DeviceID]*Device
	// TODO: Last updated
	devicesTracking   bool
	devicesOutdated   bool
	devicesLastUpdate int64
}

func NewUserDevices(userID UserID) *UserDevices {
	return &UserDevices{
		ID:      userID,
		Devices: make(map[DeviceID]*Device),
	}
}

type MyUserDevice struct {
	ID     UserID
	Device *MyDevice
}

type SessionsID struct {
	olmSessionID      SessionID
	megolmInSessionID SessionID
}

type Room struct {
	id    RoomID
	name  string
	Users map[UserID]*User
	// TODO: encryption type
	encryptionAlg EncryptionAlg
	//MegolmOutSession *olm.OutboundGroupSession
}

func NewRoom(roomID RoomID) *Room {
	return &Room{
		id:    roomID,
		Users: make(map[UserID]*User),
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
	return r.SendMsg("m.room.message", gomatrix.TextMessage{MsgType: "m.text", Body: text})
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

func (r *Room) sendOlmMsg(eventType string, contentJSON interface{}) error {
	for userID, _ := range r.Users {
		userDevices, ok := store.users[userID]
		if !ok {
			// TODO: Get list of devices from userID from cli
			continue
		}
		for _, device := range userDevices.Devices {
			contentJSONEnc := device.EncryptOlmMsg(r.id, userID, eventType, contentJSON)
			_, err := cli.SendMessageEvent(string(r.id), "m.room.encrypted",
				contentJSONEnc)
			// TODO: Figure out if we want to return early or wait
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// TODO
func (r *Room) sendMegolmMsg(eventType string, contentJSON interface{}) error {
	return fmt.Errorf("Not implemented yet")
}

func (r *Room) sendPlaintextMsg(eventType string, contentJSON interface{}) error {
	_, err := cli.SendMessageEvent(string(r.id), eventType, contentJSON)
	return err
	//return cli.SendMessageEvent(roomID, "m.room.message",
	//	TextMessage{"m.text", text})
}

type User struct {
	id   UserID
	name string
	//devices *UserDevices
}

func (u *User) Devices() *UserDevices {
	userDevices, ok := store.users[u.id]
	if ok {
		return userDevices
	} else {
		// TODO: Maybe request devices?
		// TODO: Maybe do it asynchronously?
		return nil
	}
}

// RoomsSessionsID maps (RoomID, UserID, DeviceID) to (olmSessionID, megolmInSessionID)
type RoomsSessionsID struct {
	roomIDuserIDdeviceID map[RoomID]map[UserID]map[DeviceID]*SessionsID
}

func (rs *RoomsSessionsID) getSessionsID(roomID RoomID, userID UserID,
	deviceID DeviceID) *SessionsID {
	room, ok := rs.roomIDuserIDdeviceID[roomID]
	if !ok {
		return nil
	}
	user, ok := room[userID]
	if !ok {
		return nil
	}
	sessionsID, ok := user[deviceID]
	if !ok {
		return nil
	}
	return sessionsID
}

func (rs *RoomsSessionsID) makeSessionsID(roomID RoomID, userID UserID,
	deviceID DeviceID) *SessionsID {
	room, ok := rs.roomIDuserIDdeviceID[roomID]
	if !ok {
		rs.roomIDuserIDdeviceID[roomID] = make(map[UserID]map[DeviceID]*SessionsID)
		room = rs.roomIDuserIDdeviceID[roomID]
	}
	user, ok := room[userID]
	if !ok {
		room[userID] = make(map[DeviceID]*SessionsID)
		user = room[userID]
	}
	sessionsID, ok := user[deviceID]
	if !ok {
		user[deviceID] = &SessionsID{}
		sessionsID = user[deviceID]
	}
	return sessionsID
}

func (rs *RoomsSessionsID) GetOlmSessionID(roomID RoomID, userID UserID, deviceID DeviceID) SessionID {
	sessionsID := rs.getSessionsID(roomID, userID, deviceID)
	if sessionsID == nil {
		return ""
	} else {
		return sessionsID.olmSessionID
	}
}

func (rs *RoomsSessionsID) setOlmSessionID(roomID RoomID, userID UserID, deviceID DeviceID,
	sessionID SessionID) {
	sessionsID := rs.makeSessionsID(roomID, userID, deviceID)
	sessionsID.olmSessionID = sessionID
}

func (rs *RoomsSessionsID) setMegolmSessionID(roomID RoomID, userID UserID, deviceID DeviceID,
	sessionID SessionID) {
	sessionsID := rs.makeSessionsID(roomID, userID, deviceID)
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
func (cdb *CryptoDB) AddUser(userID UserID) error {
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
func (cdb *CryptoDB) AddUserDevice(userID UserID, deviceID DeviceID) error {
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
func (cdb *CryptoDB) AddMyUserMyDevice(userID UserID, deviceID DeviceID) error {
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
func (cdb *CryptoDB) StorePubKeys(userID UserID, deviceID DeviceID,
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
func (cdb *CryptoDB) StoreOlmSession(userID UserID, deviceID DeviceID,
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
func (cdb *CryptoDB) StoreMegolmInSession(userID UserID, deviceID DeviceID,
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
func (cdb *CryptoDB) StoreMegolmOutSession(userID UserID, deviceID DeviceID,
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

func deviceFromBucket(deviceID DeviceID, deviceBucket *bolt.Bucket) (*Device, error) {
	device := &Device{
		ID:               deviceID,
		Ed25519:          string(deviceBucket.Get([]byte("ed25519"))),
		Curve25519:       string(deviceBucket.Get([]byte("curve25519"))),
		OlmSessions:      make(map[SessionID]*olm.Session),
		MegolmInSessions: make(map[SessionID]*olm.InboundGroupSession),
	}
	olmSessionsBucket := deviceBucket.Bucket([]byte("olm"))
	err := olmSessionsBucket.ForEach(func(sessionID, session []byte) error {
		var err error
		device.OlmSessions[SessionID(sessionID)], err =
			olm.SessionFromPickled(string(session), []byte(""))
		return err
	})
	if err != nil {
		return nil, err
	}
	megolmInSessionsBucket := deviceBucket.Bucket([]byte("megolm_in"))
	err = megolmInSessionsBucket.ForEach(func(sessionID, session []byte) error {
		var err error
		device.MegolmInSessions[SessionID(sessionID)], err =
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

func userDevicesFromBucket(userID UserID, userBucket *bolt.Bucket) (*UserDevices, error) {
	user := NewUserDevices(userID)
	var err error
	user.devicesTracking = bytes2bool(userBucket.Get([]byte("devices_tracking")))
	user.devicesOutdated = bytes2bool(userBucket.Get([]byte("devices_outdated")))
	user.devicesLastUpdate = bytes2int64(userBucket.Get([]byte("devices_last_update")))
	devicesBucket := userBucket.Bucket([]byte("devices"))
	err = devicesBucket.ForEach(func(deviceID, v []byte) error {
		deviceBucket := devicesBucket.Bucket(deviceID)
		device, err := deviceFromBucket(DeviceID(deviceID), deviceBucket)
		user.Devices[DeviceID(deviceID)] = device
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
func (cdb *CryptoDB) LoadAllUserDevices() (map[UserID]*UserDevices, error) {
	users := make(map[UserID]*UserDevices)
	err := cdb.db.View(func(tx *bolt.Tx) error {
		cryptoUsersBucket := tx.Bucket([]byte("crypto_users"))
		err := cryptoUsersBucket.ForEach(func(userID, v []byte) error {
			userBucket := cryptoUsersBucket.Bucket([]byte(userID))
			user, err := userDevicesFromBucket(UserID(userID), userBucket)
			users[UserID(userID)] = user
			return err
		})
		return err
	})
	return users, err
}

func myDeviceFromBucket(deviceID DeviceID, deviceBucket *bolt.Bucket) (*MyDevice, error) {
	device := &MyDevice{
		ID:                DeviceID(deviceID),
		Ed25519:           string(deviceBucket.Get([]byte("ed25519"))),
		Curve25519:        string(deviceBucket.Get([]byte("curve25519"))),
		MegolmOutSessions: make(map[RoomID]*olm.OutboundGroupSession),
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
		device.MegolmOutSessions[RoomID(roomID)], err =
			olm.OutboundGroupSessionFromPickled(string(session), []byte(""))
		return err
	})
	return device, err
}

// LoadMyUserDevice loads the MyUserDevice at /crypto_me/<userID>/
func (cdb *CryptoDB) LoadMyUserDevice(userID UserID) (*MyUserDevice, error) {
	myUser := &MyUserDevice{
		ID: userID,
	}
	err := cdb.db.View(func(tx *bolt.Tx) error {
		userBucket := tx.Bucket([]byte("crypto_me")).Bucket([]byte(userID))
		// Load the first device in the bucket (there should only be one)
		err := userBucket.ForEach(func(deviceID, v []byte) error {
			deviceBucket := userBucket.Bucket(deviceID)
			device, err := myDeviceFromBucket(DeviceID(deviceID), deviceBucket)
			myUser.Device = device
			return err
		})
		return err
	})
	return myUser, err
}

// ExistsOlmAccount checks if an olm.Account exists at /crypto_me/<userID>/<deviceID>/
func (cdb *CryptoDB) ExistsOlmAccount(userID UserID, deviceID DeviceID) bool {
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
func (cdb *CryptoDB) StoreOlmAccount(userID UserID, deviceID DeviceID, olmAccount *olm.Account) error {
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
func (cdb *CryptoDB) StoreOlmSessioID(roomID RoomID, userID UserID, deviceID DeviceID,
	sessionID SessionID) error {
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
		deviceBucket, err := userBucket.CreateBucketIfNotExists([]byte(deviceID))
		if err != nil {
			return fmt.Errorf("create bucket: %s", err)
		}
		deviceBucket.Put([]byte("olm_session_id"), []byte(sessionID))
		return nil
	})
	return err
}

func updateMapSessionsIDFromBucket(roomID RoomID, userID UserID, deviceID DeviceID,
	deviceBucket *bolt.Bucket, sessionsID *RoomsSessionsID) {
	olmSessionID := deviceBucket.Get([]byte("olm_session_id"))
	if olmSessionID != nil {
		sessionsID.setOlmSessionID(roomID, userID, deviceID, SessionID(olmSessionID))
	}
	megolmSessionID := deviceBucket.Get([]byte("megolm_session_id"))
	if megolmSessionID != nil {
		sessionsID.setMegolmSessionID(roomID, userID, deviceID, SessionID(megolmSessionID))
	}
}

// LoadMapSessionsID loads all the RoomsSessionsID at /crypto_sessions_id/
func (cdb *CryptoDB) LoadMapSessionsID() (*RoomsSessionsID, error) {
	sessionsID := RoomsSessionsID{
		roomIDuserIDdeviceID: make(map[RoomID]map[UserID]map[DeviceID]*SessionsID),
	}
	err := cdb.db.View(func(tx *bolt.Tx) error {
		cryptoRoomsBucket := tx.Bucket([]byte("crypto_sessions_id"))
		err := cryptoRoomsBucket.ForEach(func(roomID, v []byte) error {
			roomBucket := cryptoRoomsBucket.Bucket([]byte(roomID))
			err := roomBucket.ForEach(func(userID, v []byte) error {
				userBucket := roomBucket.Bucket(userID)
				err := userBucket.ForEach(func(deviceID, v []byte) error {
					deviceBucket := userBucket.Bucket(deviceID)
					updateMapSessionsIDFromBucket(RoomID(roomID),
						UserID(userID), DeviceID(deviceID),
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

func roomFromBucket(roomID RoomID, roomBucket *bolt.Bucket) *Room {
	room := NewRoom(roomID)
	room.encryptionAlg = EncryptionAlg(roomBucket.Get([]byte("encryption_alg")))
	return room
}

func (cdb *CryptoDB) LoadRooms() (map[RoomID]*Room, error) {
	rooms := make(map[RoomID]*Room)
	err := cdb.db.View(func(tx *bolt.Tx) error {
		cryptoRoomsBucket := tx.Bucket([]byte("crypto_rooms"))
		err := cryptoRoomsBucket.ForEach(func(roomID, v []byte) error {
			roomBucket := cryptoRoomsBucket.Bucket([]byte(roomID))
			rooms[RoomID(roomID)] = roomFromBucket(RoomID(roomID), roomBucket)
			rooms[RoomID(roomID)].Users = make(map[UserID]*User)
			return nil
		})
		return err
	})
	return rooms, err
}

func (cdb *CryptoDB) StoreRoomEncryptionAlg(roomID RoomID, encryptionAlg EncryptionAlg) error {
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

	cli, _ = gomatrix.NewClient(homeserver, "", "")
	cli.Prefix = "/_matrix/client/unstable"
	fmt.Println("Logging in...")
	res, err := cli.Login(&gomatrix.ReqLogin{
		Type:                     "m.login.password",
		User:                     username,
		Password:                 password,
		DeviceID:                 string(deviceID),
		InitialDeviceDisplayName: deviceDisplayName,
	})
	if err != nil {
		panic(err)
	}
	cli.SetCredentials(res.UserID, res.AccessToken)

	joinedRooms, err := cli.JoinedRooms()
	if err != nil {
		panic(err)
	}
	for i := 0; i < len(joinedRooms.JoinedRooms); i++ {
		roomID := RoomID(joinedRooms.JoinedRooms[i])
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
			room.Users[UserID(userID)] = &User{
				id:   UserID(userID),
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
	var roomID RoomID
	var theirUserID UserID
	if err != nil {
		fmt.Println("Creating new room...")
		fmt.Printf("Write user ID to invite: ")
		fmt.Scanln(&input)
		if input != "" {
			theirUserID = UserID(input)
		}
		resp, err := cli.CreateRoom(&gomatrix.ReqCreateRoom{
			Invite: []string{string(theirUserID)},
		})
		if err != nil {
			panic(err)
		}
		roomID = RoomID(resp.RoomID)
		store.rooms[roomID] = NewRoom(RoomID(roomID))
	} else {
		roomID = RoomID(joinedRooms.JoinedRooms[roomIdx])
		fmt.Println("Selected room is", roomID)
		for userID, _ := range store.rooms[RoomID(roomID)].Users {
			if userID == store.me.ID {
				continue
			}
			theirUserID = userID
			break
		}
	}

	room := store.rooms[roomID]

	var theirUser *UserDevices
	var ok bool
	if theirUser, ok = store.users[theirUserID]; !ok {
		theirUser = NewUserDevices(theirUserID)
		store.users[theirUser.ID] = theirUser
		store.db.AddUser(theirUser.ID)
	}

	if len(theirUser.Devices) == 0 {
		respQuery, err := cli.KeysQuery(map[string][]string{string(theirUser.ID): []string{}}, -1)
		if err != nil {
			panic(err)
		}
		fmt.Printf("%+v\n", respQuery)
		for theirDeviceID, deviceKeys := range respQuery.DeviceKeys[string(theirUser.ID)] {
			device := NewDevice(DeviceID(theirDeviceID))
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
			store.db.AddUserDevice(theirUser.ID, DeviceID(theirDeviceID))
			store.db.StorePubKeys(theirUser.ID, device.ID, device.Ed25519, device.Curve25519)
			theirUser.Devices[device.ID] = device
		}
	}

	deviceKeysAlgorithms := map[string]map[string]string{string(theirUser.ID): map[string]string{}}
	keysToClaim := 0
	for theirDeviceID, _ := range theirUser.Devices {
		if store.sessionsID.GetOlmSessionID(room.id, theirUser.ID, theirDeviceID) == "" {
			deviceKeysAlgorithms[string(theirUser.ID)][string(theirDeviceID)] = "signed_curve25519"
			keysToClaim++
		}
	}

	if keysToClaim > 0 {
		fmt.Printf("%+v\n", deviceKeysAlgorithms)
		respClaim, err := cli.KeysClaim(deviceKeysAlgorithms, -1)
		if err != nil {
			panic(err)
		}
		fmt.Printf("%+v\n", respClaim)

		var oneTimeKey string
		for theirDeviceID, _ := range deviceKeysAlgorithms[string(theirUser.ID)] {
			algorithmKey, ok := respClaim.OneTimeKeys[string(theirUser.ID)][theirDeviceID]
			if !ok {
				panic(fmt.Sprint("One time key for device", theirDeviceID, "not returned"))
			}
			for algorithmKeyID, rawOTK := range algorithmKey {
				algorithm, _ := SplitAlgorithmKeyID(algorithmKeyID)
				switch algorithm {
				case "signed_curve25519":
					var OTK SignedKey
					err := mapstructure.Decode(rawOTK, &OTK)
					if err != nil {
						panic(err)
					}
					//fmt.Printf("OTK: %+v\n", OTK)
					device, ok := theirUser.Devices[DeviceID(theirDeviceID)]
					if ok {
						oneTimeKey = OTK.Key
						olmSession, err := store.me.Device.OlmAccount.NewOutboundSession(device.Curve25519,
							oneTimeKey)
						if err != nil {
							panic(err)
						}
						store.sessionsID.setOlmSessionID(room.id, theirUser.ID, device.ID, SessionID(olmSession.ID()))
						store.db.StoreOlmSessioID(room.id, theirUser.ID, device.ID, SessionID(olmSession.ID()))
						device.OlmSessions[SessionID(olmSession.ID())] = olmSession
						store.db.StoreOlmSession(theirUser.ID, device.ID, olmSession)
					}
				}
			}
		}
		fmt.Printf("%+v\n", theirUser)
		for _, device := range theirUser.Devices {
			fmt.Printf("%+v\n", *device)
		}
	}

	if room.EncryptionAlg() != EncryptionAlgOlm {
		err = room.SetOlmEncryption()
		if err != nil {
			panic(err)
		}
	}

	text := fmt.Sprint("I'm encrypted :D ~ ", time.Now().Format("2006-01-02 15:04:05"))
	err = room.SendText(text)
	if err != nil {
		panic(err)
	}

	//res, err := c.cli.SyncRequest(30000, "", "", false, "online")
	//if err != nil {
	//	return err
	//}
	//for {
	//	res, err = cli.SyncRequest(30000, res.NextBatch, "", false, "online")
	//	if err != nil {
	//		time.Sleep(10)
	//		continue
	//	}
	//	Update(res)
	//}
}

//
//func Update(res *gomatrix.RespSync) {
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
