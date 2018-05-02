package main

import (
	"encoding/binary"
	"fmt"
	mat "github.com/Dhole/gomatrix"
	"github.com/boltdb/bolt"
	olm "gitlab.com/dhole/go-olm"
	"time"
)

type Storer interface {
	Close()
	AddUser(userID mat.UserID) error
	AddUserDevice(userID mat.UserID, deviceID mat.DeviceID) error
	AddMyUserMyDevice(userID mat.UserID, deviceID mat.DeviceID) error
	StorePubKeys(userID mat.UserID, deviceID mat.DeviceID,
		ed25519 olm.Ed25519, curve25519 olm.Curve25519) error
	StoreOlmSession(userID mat.UserID, deviceID mat.DeviceID,
		olmSession *olm.Session) error
	StoreMegolmInSession(userID mat.UserID, deviceID mat.DeviceID,
		megolmInSession *olm.InboundGroupSession) error
	SetSharedMegolmOutKey(userID mat.UserID, deviceID mat.DeviceID,
		sessionID olm.SessionID) error
	StoreMegolmOutSession(userID mat.UserID, deviceID mat.DeviceID,
		megolmOutSession *olm.OutboundGroupSession) error
	LoadAllUserDevices() (map[mat.UserID]*UserDevices, error)
	LoadMyUserDevice(userID mat.UserID) (*MyUserDevice, error)
	ExistsOlmAccount(userID mat.UserID, deviceID mat.DeviceID) bool
	StoreOlmAccount(userID mat.UserID, deviceID mat.DeviceID, olmAccount *olm.Account) error
	StoreOlmSessionID(roomID mat.RoomID, userID mat.UserID, key olm.Curve25519,
		sessionID olm.SessionID) error
	StoreMegolmInSessionID(roomID mat.RoomID, userID mat.UserID, key olm.Curve25519,
		sessionID olm.SessionID) error
	LoadMapSessionsID() (*RoomsSessionsID, error)
	LoadRooms() (map[mat.RoomID]*Room, error)
	StoreRoomEncryptionAlg(roomID mat.RoomID, encryptionAlg olm.Algorithm) error
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

// AddUserDevice adds
// /crypto_users/<userID>/devices/<deviceID>/{olm,megolm_in,shared_megolm}/ buckets
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
		_, err = deviceBucket.CreateBucketIfNotExists([]byte("shared_megolm"))
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
	ed25519 olm.Ed25519, curve25519 olm.Curve25519) error {
	err := cdb.db.Update(func(tx *bolt.Tx) error {
		deviceBucket := getBuckets(tx, "crypto_users", userID, "devices", deviceID)
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
		olmSessionsBucket :=
			getBuckets(tx, "crypto_users", userID, "devices", deviceID, "olm")
		olmSessionsBucket.Put([]byte(olmSession.ID()), []byte(olmSession.Pickle([]byte(""))))
		return nil
	})
	return err
}

func getBuckets(bucket interface{}, keys ...interface{}) (b *bolt.Bucket) {
	for i, _key := range keys {
		var key string
		switch v := _key.(type) {
		case string:
			key = v
		case mat.RoomID:
			key = string(v)
		case mat.UserID:
			key = string(v)
		case mat.DeviceID:
			key = string(v)
		case olm.SessionID:
			key = string(v)
		case olm.Curve25519:
			key = string(v)
		case olm.Ed25519:
			key = string(v)
		case olm.Algorithm:
			key = string(v)
		default:
			panic(fmt.Sprintf("Type %T not handled for bucket key", _key))
		}
		if i == 0 {
			switch v := bucket.(type) {
			case *bolt.Bucket:
				b = v.Bucket([]byte(key))
			case *bolt.Tx:
				b = v.Bucket([]byte(key))
			}
		} else {
			b = b.Bucket([]byte(key))
		}
	}
	return
}

// StoreMegolmInSession stores an olm.InboundGroupSession at
// /crypto_users/<userID>/devices/<deviceID>/megolm_in/<megolmInSession.ID>
func (cdb *CryptoDB) StoreMegolmInSession(userID mat.UserID, deviceID mat.DeviceID,
	megolmInSession *olm.InboundGroupSession) error {
	err := cdb.db.Update(func(tx *bolt.Tx) error {
		megolmInSessionsBucket :=
			getBuckets(tx, "crypto_users", userID, "devices", deviceID, "megolm_in")
		megolmInSessionsBucket.Put([]byte(megolmInSession.ID()),
			[]byte(megolmInSession.Pickle([]byte(""))))
		return nil
	})
	return err
}

func (cdb *CryptoDB) SetSharedMegolmOutKey(userID mat.UserID, deviceID mat.DeviceID,
	sessionID olm.SessionID) error {
	err := cdb.db.Update(func(tx *bolt.Tx) error {
		megolmSharedBucket :=
			getBuckets(tx, "crypto_users", userID, "devices", deviceID, "shared_megolm")
		megolmSharedBucket.Put([]byte(sessionID),
			bool2bytes(true))
		return nil
	})
	return err
}

// StoreMegolmOutSession stores an olm.OutboundGroupSession at
// /crypto_me/<userID>/<deviceID>/megolm_out/<megolmOutSession.ID>
func (cdb *CryptoDB) StoreMegolmOutSession(userID mat.UserID, deviceID mat.DeviceID,
	megolmOutSession *olm.OutboundGroupSession) error {
	err := cdb.db.Update(func(tx *bolt.Tx) error {
		megolmOutSessionsBucket :=
			getBuckets(tx, "crypto_me", userID, deviceID, "megolm_out")
		megolmOutSessionsBucket.Put([]byte(megolmOutSession.ID()),
			[]byte(megolmOutSession.Pickle([]byte(""))))
		return nil
	})
	return err
}

func deviceFromBucket(deviceID mat.DeviceID, deviceBucket *bolt.Bucket) (*Device, error) {
	device := &Device{
		ID:               deviceID,
		Ed25519:          olm.Ed25519(deviceBucket.Get([]byte("ed25519"))),
		Curve25519:       olm.Curve25519(deviceBucket.Get([]byte("curve25519"))),
		OlmSessions:      make(map[olm.SessionID]*olm.Session),
		MegolmInSessions: make(map[olm.SessionID]*olm.InboundGroupSession),
	}
	olmSessionsBucket := deviceBucket.Bucket([]byte("olm"))
	err := olmSessionsBucket.ForEach(func(sessionID, session []byte) error {
		var err error
		device.OlmSessions[olm.SessionID(sessionID)], err =
			olm.SessionFromPickled(string(session), []byte(""))
		return err
	})
	if err != nil {
		return nil, err
	}
	megolmInSessionsBucket := deviceBucket.Bucket([]byte("megolm_in"))
	err = megolmInSessionsBucket.ForEach(func(sessionID, session []byte) error {
		var err error
		device.MegolmInSessions[olm.SessionID(sessionID)], err =
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
		Ed25519:           olm.Ed25519(deviceBucket.Get([]byte("ed25519"))),
		Curve25519:        olm.Curve25519(deviceBucket.Get([]byte("curve25519"))),
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
		userBucket := getBuckets(tx, "crypto_me", userID)
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
		deviceBucket := getBuckets(tx, "crypto_me", userID, deviceID)
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
	ed25519, curve25519 := olmAccount.IdentityKeys()

	err := cdb.db.Update(func(tx *bolt.Tx) error {
		deviceBucket := getBuckets(tx, "crypto_me", userID, deviceID)
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

// StoreOlmSessionID stores an olm SessionID at
// /crypto_sessions_id/<roomID>/<userID>/<deviceID>/olm_session_id
func (cdb *CryptoDB) StoreOlmSessionID(roomID mat.RoomID, userID mat.UserID, key olm.Curve25519,
	sessionID olm.SessionID) error {
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

// StoreMegolmInSessionID stores an olm.Account at
// /crypto_sessions_id/<roomID>/<userID>/<deviceID>/megolm_session_id
func (cdb *CryptoDB) StoreMegolmInSessionID(roomID mat.RoomID, userID mat.UserID,
	key olm.Curve25519, sessionID olm.SessionID) error {
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
		deviceBucket.Put([]byte("megolm_session_id"), []byte(sessionID))
		return nil
	})
	return err
}

func updateMapSessionsIDFromBucket(roomID mat.RoomID, userID mat.UserID, key olm.Curve25519,
	deviceBucket *bolt.Bucket, sessionsID *RoomsSessionsID) {
	olmSessionID := deviceBucket.Get([]byte("olm_session_id"))
	if olmSessionID != nil {
		sessionsID.setOlmSessionID(roomID, userID, key, olm.SessionID(olmSessionID))
	}
	megolmSessionID := deviceBucket.Get([]byte("megolm_session_id"))
	if megolmSessionID != nil {
		sessionsID.setMegolmSessionID(roomID, userID, key, olm.SessionID(megolmSessionID))
	}
}

// LoadMapSessionsID loads all the RoomsSessionsID at /crypto_sessions_id/
func (cdb *CryptoDB) LoadMapSessionsID() (*RoomsSessionsID, error) {
	sessionsID := RoomsSessionsID{
		roomIDuserIDKey: make(map[mat.RoomID]map[mat.UserID]map[olm.Curve25519]*SessionsID),
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
						mat.UserID(userID), olm.Curve25519(key),
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
	room.encryptionAlg = olm.Algorithm(roomBucket.Get([]byte("encryption_alg")))
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

func (cdb *CryptoDB) StoreRoomEncryptionAlg(roomID mat.RoomID, encryptionAlg olm.Algorithm) error {
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
