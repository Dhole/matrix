package main

import (
	"encoding/json"
	"fmt"
	"github.com/boltdb/bolt"
	"github.com/matrix-org/gomatrix"
	"github.com/mitchellh/mapstructure"
	olm "gitlab.com/dhole/go-olm"
	"log"
	"strings"
	"time"
)

var userID = "@ray_test:matrix.org"
var username = "ray_test"
var homeserver = "https://matrix.org"
var password = "CiIYIrD3OtSuudJB"
var deviceID = "5un3HpnWE"
var deviceDisplayName = "go-olm-dev"

type Device struct {
	ID          string
	SigningKey  string // Ed25519
	IdentityKey string // OlmCurve25519
	//OneTimeKey       string                              // OlmCurve25519
	OlmSessions      map[string]*olm.Session             // key:session_id
	MegolmInSessions map[string]*olm.InboundGroupSession // key:session_id
}

type MyDevice struct {
	ID          string
	SigningKey  string // Ed25519
	IdentityKey string // OlmCurve25519
	OlmAccount  *olm.Account
	//OlmSessions       map[string]*olm.Session              // key:room_id
	MegolmOutSessions map[string]*olm.OutboundGroupSession // key:room_id

}

type User struct {
	ID      string
	Devices map[string]*Device
}

type MyUser struct {
	ID     string
	Device *MyDevice
}

type SignedKey struct {
	Key        string                       `json:"key"`
	Signatures map[string]map[string]string `json:"signatures"`
}

type CryptoDB struct {
	db *bolt.DB
}

// OpenCryptoDB opens the DB and initializes the /crypto bucket if necessary
func OpenCryptoDB(filename string) (CryptoDB, error) {
	var cdb CryptoDB
	db, err := bolt.Open(filename, 0660, &bolt.Options{Timeout: 200 * time.Millisecond})
	cdb.db = db
	if err != nil {
		return cdb, err
	}
	// Create base buckets
	err := cdb.db.Update(func(tx *bolt.Tx) error {
		cryptoBucket, err := tx.CreateBucketIfNotExists([]byte("crypto"))
		if err != nil {
			return fmt.Errorf("create bucket: %s", err)
		}
		return nil
	})
	return cdb, err
}

// Close closes the DB
func (cdb *CryptoDB) Close() {
	cdb.db.Close()
}

// ExistsUserDevice checks if /crypto/<userID>/<deviceID>/ exists
func (cdb *CryptoDB) ExistsUserDevice(userID, deviceID string) bool {
	deviceExists := false
	cdb.db.Update(func(tx *bolt.Tx) error {
		cryptoBucket := tx.Bucket([]byte("crypto"))
		userBucket := cryptoBucket.Bucket([]byte(userID))
		if userBucket != nil {
			return nil
		}
		deviceBucket := userBucket.Bucket([]byte(deviceID))
		if deviceBucket != nil {
			return nil
		}
		deviceExists = true
		return nil
	})
	return deviceExists
}

// AddMyUserMyDevice adds /crypto/<userID>/<deviceID>/megolm_out/ buckets
func (cdb *CryptoDB) AddMyUserMyDevice(userID, deviceID string) error {
	err := cdb.db.Update(func(tx *bolt.Tx) error {
		cryptoBucket := tx.Bucket([]byte("crypto"))
		userBucket, err := cryptoBucket.CreateBucketIfNotExists([]byte(userID))
		if err != nil {
			return fmt.Errorf("create bucket: %s", err)
		}
		_, err := userBucket.CreateBucketIfNotExists([]byte(deviceID))
		if err != nil {
			return fmt.Errorf("create bucket: %s", err)
		}
		_, err := userBucket.CreateBucketIfNotExists([]byte("megolm_out"))
		if err != nil {
			return fmt.Errorf("create bucket: %s", err)
		}
		return nil
	})
	return err
}

// AddUserDevice adds /crypto/<userID>/<deviceID>/{olm,megolm_in}/ buckets
func (cdb *CryptoDB) AddUserDevice(userID, deviceID string) error {
	err := cdb.db.Update(func(tx *bolt.Tx) error {
		cryptoBucket := tx.Bucket([]byte("crypto"))
		userBucket, err := cryptoBucket.CreateBucketIfNotExists([]byte(userID))
		if err != nil {
			return fmt.Errorf("create bucket: %s", err)
		}
		_, err := userBucket.CreateBucketIfNotExists([]byte(deviceID))
		if err != nil {
			return fmt.Errorf("create bucket: %s", err)
		}
		_, err := userBucket.CreateBucketIfNotExists([]byte("olm"))
		if err != nil {
			return fmt.Errorf("create bucket: %s", err)
		}
		_, err := userBucket.CreateBucketIfNotExists([]byte("megolm_in"))
		if err != nil {
			return fmt.Errorf("create bucket: %s", err)
		}
		return nil
	})
	return err
}

// StorePubKeys stores the ed25519 and curve25519 public keys at /crypto/<userID>/<deviceID>/
func (cdb *CryptoDB) StorePubKeys(userID, deviceID, ed25519, curve25519 string) error {
	err := db.Update(func(tx *bolt.Tx) error {
		deviceBucket := tx.Bucket([]byte("crypto")).Bucket([]byte(userID)).Bucket([]byte(deviceID))
		deviceBucket.Put([]byte("ed25519"), []byte(ed25519))
		deviceBucket.Put([]byte("curve25519"), []byte(curve25519))
		return nil
	})
	return err
}

// StoreOlmSession stores an olm.Session at /crypto/<userID>/<deviceID>/olm/<olmSession.ID>
func (cdb *CryptoDB) StoreOlmSession(userID, deviceID string, olmSession *olm.Session) error {
	err := db.Update(func(tx *bolt.Tx) error {
		olmSessionsBucket := tx.Bucket([]byte("crypto")).Bucket([]byte(userID)).Bucket([]byte(deviceID)).Bucket([]byte("olm"))
		olmSessionsBucket.Put([]byte(olmSession.ID()), []byte(olmSession.Pickle([]byte(""))))
		return nil
	})
	return err
}

// StoreMegolmInSession stores an olm.InboundGroupSession at /crypto/<userID>/<deviceID>/megolm_in/<megolmInSession.ID>
func (cdb *CryptoDB) StoreMegolmInSession(userID, deviceID string, megolmInSession *olm.InboundGroupSession) error {
}

// StoreMegolmOutSession stores an olm.OutboundGroupSession at /crypto/<userID>/<deviceID>/megolm_out/<megolmOutSession.ID>
func (cdb *CryptoDB) StoreMegolmOutSession(userID, deviceID string, megolmOutSession *olm.OutboundGroupSession) error {
}

// LoadUser loads the User at /crypto/<userID>/
func (cdb *CryptoDB) LoadUser(userID string) (*User, error) {
	var user User
	err := db.View(func(tx *bolt.Tx) error {
		userBucket := tx.Bucket([]byte("crypto")).Bucket([]byte(userID))
		err := userBucket.ForEach(func(deviceID, v []byte) error {
			deviceBucket := userBucket.Bucket(deviceID)
			device := &Device{
				ID:          string(deviceID),
				IdentityKey: string(deviceBucket.Get([]byte("ed25519"))),
				SigningKey:  string(deviceBucket.Get([]byte("curve25519"))),
			}
			olmSessionsBucket := deviceBucket.Bucket([]byte("olm"))
			err := olmSessionsBucket.ForEach(func(sessionID, session []byte) error {
				device.OlmSessions[string(sessionID)] = &SessionFromPickled(string(session))
			})
			if err != nil {
				return err
			}
			megolmInSessionsBucket := deviceBucket.Bucket([]byte("megolm_in"))
			err := megolmInSessionsBucket.ForEach(func(sessionID, session []byte) error {
				device.MegolmInSessions[string(sessionID)] = &olm.InboundGroupSessionFromPickled(string(session))
			})
			if err != nil {
				return err
			}
			user.Devices[string(deviceID)] = device
			return nil
		})
		if err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &user, nil
}

// LoadMyUser loads the MyUser at /crypto/<userID>/
func (cdb *CryptoDB) LoadMyUser(userID string) (*MyUser, error) {
	var user MyUser
	err := db.View(func(tx *bolt.Tx) error {
		userBucket := tx.Bucket([]byte("crypto")).Bucket([]byte(userID))
		err := userBucket.ForEach(func(deviceID, v []byte) error {
			deviceBucket := userBucket.Bucket(deviceID)
			device := &MyDevice{
				ID:          string(deviceID),
				IdentityKey: string(deviceBucket.Get([]byte("ed25519"))),
				SigningKey:  string(deviceBucket.Get([]byte("curve25519"))),
			}
			device.OlmAccount = &olm.AccountFromPickled(string(deviceBucket.Get([]byte("account"))))
			megolmOutSessionsBucket := deviceBucket.Bucket([]byte("megolm_out"))
			err := megolmOutSessionsBucket.ForEach(func(sessionID, session []byte) error {
				device.MegolmOutSessions[string(sessionID)] = &olm.OutboundGroupSessionFromPickled(string(session))
			})
			if err != nil {
				return err
			}
			user.Devices[string(deviceID)] = device
			return nil
		})
		if err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &user, nil
}

// StoreOlmAccount stores an olm.Account at /crypto/<userID>/<deviceID>/
func (cdb *CryptoDB) StoreOlmAccount(userID, deviceID string, olmAccount *olm.Account) error {
	ed25519, curve25519 := olmAccount.IdentityKeys()

	err := db.Update(func(tx *bolt.Tx) error {
		deviceBucket := tx.Bucket([]byte("crypto")).Bucket([]byte(userID)).Bucket([]byte(deviceID))
		deviceBucket.Put([]byte("ed25519"), []byte(ed25519))
		deviceBucket.Put([]byte("curve25519"), []byte(curve25519))
		deviceBucket.Put([]byte("account"), []byte(olmAccount.Pickle([]byte(""))))
		return nil
	})
	return err
}

// LoadOlmAccount loads the olm.Account at /crypto/<userID>/<deviceID>/
func (cdb *CryptoDB) LoadOlmAccount(userID, deviceID string) (*olm.Account, error) {
	identityKeys := map[string]string{}
	var pickledAccount string
	db.Update(func(tx *bolt.Tx) error {
		deviceBucket := tx.Bucket([]byte("crypto")).Bucket([]byte(userID)).Bucket([]byte(deviceID))
		identityKeys["ed25519"] = string(deviceBucket.Get([]byte("ed25519")))
		identityKeys["curve25519"] = string(deviceBucket.Get([]byte("curve25519")))
		pickledAccount = string(deviceBucket.Get([]byte("account")))
		return nil
	})
	olmAccount, err := olm.AccountFromPickled(pickledAccount, []byte(""))
	if err != nil {
		return nil, err
	}
	return olmAccount, nil
}

func SplitAlgorithmKeyID(algorithmKeyID string) (string, string) {
	algorithmKeyIDSlice := strings.Split(algorithmKeyID, ":")
	if len(algorithmKeyIDSlice) != 2 {
		return "", ""
	}
	return algorithmKeyIDSlice[0], algorithmKeyIDSlice[1]
}

func main() {
	db, err := OpenCryptoDB("test.db")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	//// Create base buckets, create my user and device buckets, check for existence of an olm account
	//userCryptoExists := false
	//db.Update(func(tx *bolt.Tx) error {
	//	cryptoBucket, err := tx.CreateBucketIfNotExists([]byte("crypto"))
	//	if err != nil {
	//		return fmt.Errorf("create bucket: %s", err)
	//	}
	//	userBucket, err := cryptoBucket.CreateBucketIfNotExists([]byte(userID))
	//	if err != nil {
	//		return fmt.Errorf("create bucket: %s", err)
	//	}
	//	deviceBucket, err := userBucket.CreateBucketIfNotExists([]byte(deviceID))
	//	if err != nil {
	//		return fmt.Errorf("create bucket: %s", err)
	//	}
	//	v := deviceBucket.Get([]byte("ed25519"))
	//	if v != nil {
	//		userCryptoExists = true
	//	}
	//	return nil
	//})

	//// Create a new olm account if it doesn't exists and store it in the DB
	//if !userCryptoExists {
	//	olmAccount := olm.NewAccount()
	//	identityKeysJSON := olmAccount.IdentityKeys()
	//	identityKeys := map[string]string{}
	//	err = json.Unmarshal([]byte(identityKeysJSON), &identityKeys)
	//	if err != nil {
	//		panic(err)
	//	}

	//	db.Update(func(tx *bolt.Tx) error {
	//		deviceBucket := tx.Bucket([]byte("crypto")).Bucket([]byte(userID)).Bucket([]byte(deviceID))
	//		deviceBucket.Put([]byte("ed25519"), []byte(identityKeys["ed25519"]))
	//		deviceBucket.Put([]byte("curve25519"), []byte(identityKeys["curve25519"]))
	//		deviceBucket.Put([]byte("account"), []byte(olmAccount.Pickle([]byte(""))))
	//		return nil
	//	})
	//}

	// Load my user olm account from DB
	//identityKeys := map[string]string{}
	//var pickledAccount string
	//db.Update(func(tx *bolt.Tx) error {
	//	deviceBucket := tx.Bucket([]byte("crypto")).Bucket([]byte(userID)).Bucket([]byte(deviceID))
	//	identityKeys["ed25519"] = string(deviceBucket.Get([]byte("ed25519")))
	//	identityKeys["curve25519"] = string(deviceBucket.Get([]byte("curve25519")))
	//	pickledAccount = string(deviceBucket.Get([]byte("account")))
	//	return nil
	//})
	//olmAccount, err := olm.AccountFromPickled(pickledAccount, []byte(""))
	//if err != nil {
	//	panic(err)
	//}
	//fmt.Println("Identity keys:", olmAccount.IdentityKeys())

	cli, _ := gomatrix.NewClient(homeserver, "", "")
	cli.Prefix = "/_matrix/client/unstable"
	fmt.Println("Logging in...")
	res, err := cli.Login(&gomatrix.ReqLogin{
		Type:                     "m.login.password",
		User:                     username,
		Password:                 password,
		DeviceID:                 deviceID,
		InitialDeviceDisplayName: deviceDisplayName,
	})
	if err != nil {
		panic(err)
	}
	cli.SetCredentials(res.UserID, res.AccessToken)
	userID := res.UserID

	var theirUser User
	theirUser.ID = "@dhole:matrix.org"
	theirUser.Devices = make(map[string]*Device)

	theirUserCryptoExists := false
	db.View(func(tx *bolt.Tx) error {
		userBucket := tx.Bucket([]byte("crypto")).Bucket([]byte(theirUser.ID))
		if userBucket != nil {
			theirUserCryptoExists = true
		}
		return nil
	})

	if !theirUserCryptoExists {
		respQuery, err := cli.KeysQuery(map[string][]string{theirUser.ID: []string{}}, -1)
		if err != nil {
			panic(err)
		}
		fmt.Printf("%+v\n", respQuery)

		db.Update(func(tx *bolt.Tx) error {
			userBucket, err := tx.Bucket([]byte("crypto")).CreateBucketIfNotExists([]byte(theirUser.ID))
			if err != nil {
				return fmt.Errorf("create bucket: %s", err)
			}
			for theirDeviceID, device := range respQuery.DeviceKeys[theirUser.ID] {
				deviceBucket, err := userBucket.CreateBucketIfNotExists([]byte(theirDeviceID))
				if err != nil {
					return fmt.Errorf("create bucket: %s", err)
				}
				if deviceBucket.Get([]byte("ed25519")) != nil {
					// We already have the keys for this device, skip replacing them
					continue
				}
				for algorithmKeyID, key := range device.Keys {
					algorithm, _ := SplitAlgorithmKeyID(algorithmKeyID)
					switch algorithm {
					case "curve25519":
						//theirDevice.IdentityKey = key
						deviceBucket.Put([]byte("ed25519"), []byte(key))
					case "ed25519":
						//theirDevice.SigningKey = key
						deviceBucket.Put([]byte("curve25519"), []byte(key))
					}
				}
				//theirUser.Devices[theirDeviceID] = theirDevice
			}
			return nil
		})
	}

	db.View(func(tx *bolt.Tx) error {
		userBucket := tx.Bucket([]byte("crypto")).Bucket([]byte(theirUser.ID))
		userBucket.ForEach(func(deviceID, v []byte) error {
			deviceBucket := userBucket.Bucket(deviceID)
			theirUser.Devices[string(deviceID)] = &Device{
				ID:          string(deviceID),
				IdentityKey: string(deviceBucket.Get([]byte("ed25519"))),
				SigningKey:  string(deviceBucket.Get([]byte("curve25519"))),
			}
			return nil
		})
		return nil
	})

	deviceKeysAlgorithms := map[string]map[string]string{theirUser.ID: map[string]string{}}
	for theirDeviceID, _ := range theirUser.Devices {
		deviceKeysAlgorithms[theirUser.ID][theirDeviceID] = "signed_curve25519"
	}

	fmt.Printf("%+v\n", deviceKeysAlgorithms)
	respClaim, err := cli.KeysClaim(deviceKeysAlgorithms, -1)
	if err != nil {
		panic(err)
	}
	fmt.Printf("%+v\n", respClaim)

	var oneTimeKey string
	for theirDeviceID, algorithmKey := range respClaim.OneTimeKeys[theirUser.ID] {
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
				device, ok := theirUser.Devices[theirDeviceID]
				if ok {
					oneTimeKey = OTK.Key
				}
			}
		}
	}
	fmt.Printf("%+v\n", theirUser)
	for _, device := range theirUser.Devices {
		fmt.Printf("%+v\n", *device)
	}

	resp, err := cli.CreateRoom(&gomatrix.ReqCreateRoom{
		Invite: []string{theirUser.ID},
	})
	roomID := resp.RoomID

	if err != nil {
		panic(err)
	}

	cli.SendMessageEvent(roomID, "m.room.message",
		gomatrix.TextMessage{MsgType: "m.text", Body: "I'm unencrypted :("})

	text := "I'm encrypted :D"

	cli.SendStateEvent(roomID, "m.room.encryption", "",
		map[string]string{"algorithm": "m.olm.v1.curve25519-aes-sha2"})

	for deviceID, device := range theirUser.Devices {
		olmSession, err := olmAccount.NewOutboundSession(device.IdentityKey,
			oneTimeKey)
		if err != nil {
			panic(err)
		}

		payload := map[string]interface{}{
			"type":           "m.room.message",
			"content":        gomatrix.TextMessage{MsgType: "m.text", Body: text},
			"recipient":      theirUser.ID,
			"sender":         userID,
			"recipient_keys": map[string]string{"ed25519": device.SigningKey},
			"room_id":        roomID}
		payloadJSON, err := json.Marshal(payload)
		if err != nil {
			panic(err)
		}
		fmt.Println(string(payloadJSON))
		encryptMsgType, encryptedMsg := olmSession.Encrypt(string(payloadJSON))

		cli.SendMessageEvent(roomID, "m.room.encrypted",
			map[string]interface{}{
				"algorithm": "m.olm.v1.curve25519-aes-sha2",
				"ciphertext": map[string]map[string]interface{}{
					device.IdentityKey: map[string]interface{}{
						"type": encryptMsgType,
						"body": encryptedMsg,
					},
				},
				"device_id":  deviceID,
				"sender_key": identityKeys["curve25519"],
				"session_id": olmSession.ID()})
	}

	//	res, err := c.cli.SyncRequest(30000, "", "", false, "online")
	//	if err != nil {
	//		return err
	//	}
	//	for {
	//		res, err = cli.SyncRequest(30000, res.NextBatch, "", false, "online")
	//		if err != nil {
	//			time.Sleep(10)
	//			continue
	//		}
	//		Update(res)
	//	}
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
