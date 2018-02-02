package main

import (
	"encoding/json"
	"fmt"
	"github.com/boltdb/bolt"
	"github.com/matrix-org/gomatrix"
	"github.com/mitchellh/mapstructure"
	olm "gitlab.com/dhole/go-olm"
	"log"
	"strconv"
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
	ID         string
	Ed25519    string // SigningKey
	Curve25519 string // IdentityKey
	//OneTimeKey       string                              // IdentityKey
	OlmSessions      map[string]*olm.Session             // key:session_id
	MegolmInSessions map[string]*olm.InboundGroupSession // key:session_id
}

type MyDevice struct {
	ID         string
	Ed25519    string // Ed25519
	Curve25519 string // IdentityKey
	OlmAccount *olm.Account
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

type SessionsID struct {
	olmSessionID      string
	megolmInSessionID string
}

type RoomsSessionsID struct {
	roomIDuserIDdeviceID map[string]map[string]map[string]*SessionsID
}

func (rs *RoomsSessionsID) getSessionsID(roomID, userID, deviceID string) *SessionsID {
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

func (rs *RoomsSessionsID) makeSessionsID(roomID, userID, deviceID string) *SessionsID {
	room, ok := rs.roomIDuserIDdeviceID[roomID]
	if !ok {
		rs.roomIDuserIDdeviceID[roomID] = make(map[string]map[string]*SessionsID)
		room = rs.roomIDuserIDdeviceID[roomID]
	}
	user, ok := room[userID]
	if !ok {
		room[userID] = make(map[string]*SessionsID)
		user = room[userID]
	}
	sessionsID, ok := user[deviceID]
	if !ok {
		user[deviceID] = &SessionsID{}
		sessionsID = user[deviceID]
	}
	return sessionsID
}

func (rs *RoomsSessionsID) GetOlmSessionID(roomID, userID, deviceID string) string {
	sessionsID := rs.getSessionsID(roomID, userID, deviceID)
	if sessionsID == nil {
		return ""
	} else {
		return sessionsID.olmSessionID
	}
}

func (rs *RoomsSessionsID) SetOlmSessionID(roomID, userID, deviceID, sessionID string) {
	sessionsID := rs.makeSessionsID(roomID, userID, deviceID)
	sessionsID.olmSessionID = sessionID
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
	err = cdb.db.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists([]byte("crypto_users"))
		if err != nil {
			return fmt.Errorf("create bucket: %s", err)
		}
		_, err = tx.CreateBucketIfNotExists([]byte("crypto_rooms"))
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

// ExistsUser checks if /crypto_users/<userID>/ exists
func (cdb *CryptoDB) ExistsUser(userID string) bool {
	userExists := false
	cdb.db.View(func(tx *bolt.Tx) error {
		cryptoUsersBucket := tx.Bucket([]byte("crypto_users"))
		userBucket := cryptoUsersBucket.Bucket([]byte(userID))
		if userBucket == nil {
			return nil
		}
		userExists = true
		return nil
	})
	return userExists
}

// AddUseradds /crypto_users/<userID>/ bucket
func (cdb *CryptoDB) AddUser(userID string) error {
	err := cdb.db.Update(func(tx *bolt.Tx) error {
		cryptoUsersBucket := tx.Bucket([]byte("crypto_users"))
		_, err := cryptoUsersBucket.CreateBucketIfNotExists([]byte(userID))
		if err != nil {
			return fmt.Errorf("create bucket: %s", err)
		}
		return nil
	})
	return err
}

// ExistsUserDevice checks if /crypto_users/<userID>/<deviceID>/ exists
func (cdb *CryptoDB) ExistsUserDevice(userID, deviceID string) bool {
	deviceExists := false
	cdb.db.View(func(tx *bolt.Tx) error {
		cryptoUsersBucket := tx.Bucket([]byte("crypto_users"))
		userBucket := cryptoUsersBucket.Bucket([]byte(userID))
		if userBucket == nil {
			return nil
		}
		deviceBucket := userBucket.Bucket([]byte(deviceID))
		if deviceBucket == nil {
			return nil
		}
		deviceExists = true
		return nil
	})
	return deviceExists
}

// AddUserDevice adds /crypto_users/<userID>/<deviceID>/{olm,megolm_in}/ buckets
func (cdb *CryptoDB) AddUserDevice(userID, deviceID string) error {
	err := cdb.db.Update(func(tx *bolt.Tx) error {
		cryptoUsersBucket := tx.Bucket([]byte("crypto_users"))
		userBucket, err := cryptoUsersBucket.CreateBucketIfNotExists([]byte(userID))
		if err != nil {
			return fmt.Errorf("create bucket: %s", err)
		}
		deviceBucket, err := userBucket.CreateBucketIfNotExists([]byte(deviceID))
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

// AddMyUserMyDevice adds /crypto_users/<userID>/<deviceID>/megolm_out/ buckets
func (cdb *CryptoDB) AddMyUserMyDevice(userID, deviceID string) error {
	err := cdb.db.Update(func(tx *bolt.Tx) error {
		cryptoUsersBucket := tx.Bucket([]byte("crypto_users"))
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
func (cdb *CryptoDB) ExistsPubKeys(userID, deviceID string) bool {
	pubKeysExist := false
	cdb.db.View(func(tx *bolt.Tx) error {
		deviceBucket := tx.Bucket([]byte("crypto_users")).Bucket([]byte(userID)).
			Bucket([]byte(deviceID))
		ed25519 := deviceBucket.Get([]byte("ed25519"))
		curve25519 := deviceBucket.Get([]byte("curve25519"))
		if ed25519 != nil && curve25519 != nil {
			pubKeysExist = true
		}
		return nil
	})
	return pubKeysExist
}

// StorePubKeys stores the ed25519 and curve25519 public keys at /crypto_users/<userID>/<deviceID>/
func (cdb *CryptoDB) StorePubKeys(userID, deviceID, ed25519, curve25519 string) error {
	err := cdb.db.Update(func(tx *bolt.Tx) error {
		deviceBucket := tx.Bucket([]byte("crypto_users")).Bucket([]byte(userID)).
			Bucket([]byte(deviceID))
		deviceBucket.Put([]byte("ed25519"), []byte(ed25519))
		deviceBucket.Put([]byte("curve25519"), []byte(curve25519))
		return nil
	})
	return err
}

// StoreOlmSession stores an olm.Session at /crypto_users/<userID>/<deviceID>/olm/<olmSession.ID>
func (cdb *CryptoDB) StoreOlmSession(userID, deviceID string, olmSession *olm.Session) error {
	err := cdb.db.Update(func(tx *bolt.Tx) error {
		olmSessionsBucket := tx.Bucket([]byte("crypto_users")).Bucket([]byte(userID)).
			Bucket([]byte(deviceID)).Bucket([]byte("olm"))
		olmSessionsBucket.Put([]byte(olmSession.ID()), []byte(olmSession.Pickle([]byte(""))))
		return nil
	})
	return err
}

// StoreMegolmInSession stores an olm.InboundGroupSession at
// /crypto_users/<userID>/<deviceID>/megolm_in/<megolmInSession.ID>
func (cdb *CryptoDB) StoreMegolmInSession(userID, deviceID string,
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
// /crypto_users/<userID>/<deviceID>/megolm_out/<megolmOutSession.ID>
func (cdb *CryptoDB) StoreMegolmOutSession(userID, deviceID string,
	megolmOutSession *olm.OutboundGroupSession) error {
	err := cdb.db.Update(func(tx *bolt.Tx) error {
		megolmOutSessionsBucket := tx.Bucket([]byte("crypto_users")).Bucket([]byte(userID)).
			Bucket([]byte(deviceID)).Bucket([]byte("megolm_out"))
		megolmOutSessionsBucket.Put([]byte(megolmOutSession.ID()),
			[]byte(megolmOutSession.Pickle([]byte(""))))
		return nil
	})
	return err
}

// LoadUser loads the User at /crypto_users/<userID>/
func (cdb *CryptoDB) LoadUser(user *User) error {
	err := cdb.db.View(func(tx *bolt.Tx) error {
		userBucket := tx.Bucket([]byte("crypto_users")).Bucket([]byte(user.ID))
		err := userBucket.ForEach(func(deviceID, v []byte) error {
			deviceBucket := userBucket.Bucket(deviceID)
			device := &Device{
				ID:               string(deviceID),
				Ed25519:          string(deviceBucket.Get([]byte("ed25519"))),
				Curve25519:       string(deviceBucket.Get([]byte("curve25519"))),
				OlmSessions:      make(map[string]*olm.Session),
				MegolmInSessions: make(map[string]*olm.InboundGroupSession),
			}
			olmSessionsBucket := deviceBucket.Bucket([]byte("olm"))
			err := olmSessionsBucket.ForEach(func(sessionID, session []byte) error {
				var err error
				device.OlmSessions[string(sessionID)], err =
					olm.SessionFromPickled(string(session), []byte(""))
				return err
			})
			if err != nil {
				return err
			}
			megolmInSessionsBucket := deviceBucket.Bucket([]byte("megolm_in"))
			err = megolmInSessionsBucket.ForEach(func(sessionID, session []byte) error {
				var err error
				device.MegolmInSessions[string(sessionID)], err =
					olm.InboundGroupSessionFromPickled(string(session), []byte(""))
				return err
			})
			if err != nil {
				return err
			}
			user.Devices[string(deviceID)] = device
			return nil
		})
		return err
	})
	return err
}

// LoadMyUser loads the MyUser at /crypto_users/<userID>/
func (cdb *CryptoDB) LoadMyUser(myUser *MyUser) error {
	err := cdb.db.View(func(tx *bolt.Tx) error {
		userBucket := tx.Bucket([]byte("crypto_users")).Bucket([]byte(myUser.ID))
		err := userBucket.ForEach(func(deviceID, v []byte) error {
			deviceBucket := userBucket.Bucket(deviceID)
			device := &MyDevice{
				ID:                string(deviceID),
				Ed25519:           string(deviceBucket.Get([]byte("ed25519"))),
				Curve25519:        string(deviceBucket.Get([]byte("curve25519"))),
				MegolmOutSessions: make(map[string]*olm.OutboundGroupSession),
			}
			var err error
			device.OlmAccount, err = olm.AccountFromPickled(
				string(deviceBucket.Get([]byte("account"))), []byte(""))
			if err != nil {
				return err
			}
			megolmOutSessionsBucket := deviceBucket.Bucket([]byte("megolm_out"))
			err = megolmOutSessionsBucket.ForEach(func(sessionID, session []byte) error {
				var err error
				device.MegolmOutSessions[string(sessionID)], err =
					olm.OutboundGroupSessionFromPickled(string(session), []byte(""))
				return err
			})
			if err != nil {
				return err
			}
			myUser.Device = device
			return nil
		})
		if err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return err
	}
	return nil
}

// ExistsOlmAccount checks if an olm.Account exists at /crypto_users/<userID>/<deviceID>/
func (cdb *CryptoDB) ExistsOlmAccount(userID, deviceID string) bool {
	olmAccountExists := false
	cdb.db.View(func(tx *bolt.Tx) error {
		deviceBucket := tx.Bucket([]byte("crypto_users")).Bucket([]byte(userID)).
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

// StoreOlmAccount stores an olm.Account at /crypto_users/<userID>/<deviceID>/
func (cdb *CryptoDB) StoreOlmAccount(userID, deviceID string, olmAccount *olm.Account) error {
	ed25519, curve25519 := olmAccount.IdentityKeysEd25519Curve25519()

	err := cdb.db.Update(func(tx *bolt.Tx) error {
		deviceBucket := tx.Bucket([]byte("crypto_users")).Bucket([]byte(userID)).
			Bucket([]byte(deviceID))
		deviceBucket.Put([]byte("ed25519"), []byte(ed25519))
		deviceBucket.Put([]byte("curve25519"), []byte(curve25519))
		deviceBucket.Put([]byte("account"), []byte(olmAccount.Pickle([]byte(""))))
		return nil
	})
	return err
}

// LoadOlmAccount loads the olm.Account at /crypto_users/<userID>/<deviceID>/
func (cdb *CryptoDB) LoadOlmAccount(userID, deviceID string) (*olm.Account, error) {
	identityKeys := map[string]string{}
	var pickledAccount string
	err := cdb.db.View(func(tx *bolt.Tx) error {
		deviceBucket := tx.Bucket([]byte("crypto_users")).Bucket([]byte(userID)).
			Bucket([]byte(deviceID))
		identityKeys["ed25519"] = string(deviceBucket.Get([]byte("ed25519")))
		identityKeys["curve25519"] = string(deviceBucket.Get([]byte("curve25519")))
		pickledAccount = string(deviceBucket.Get([]byte("account")))
		return nil
	})
	if err != nil {
		return nil, err
	}
	olmAccount, err := olm.AccountFromPickled(pickledAccount, []byte(""))
	if err != nil {
		return nil, err
	}
	return olmAccount, nil
}

// StoreOlmSessioID stores an olm.Account at
// /crypto_rooms/<roomID>/<userID>/<deviceID>/olm_session_id
func (cdb *CryptoDB) StoreOlmSessioID(roomID, userID, deviceID, sessionID string) error {
	err := cdb.db.Update(func(tx *bolt.Tx) error {
		cryptoRoomsBucket := tx.Bucket([]byte("crypto_rooms"))
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

// LoadRoomsSessionsID loads the RoomsSessionsID at /crypto_rooms/<roomID>/
func (cdb *CryptoDB) LoadRoomsSessionsID(roomID string) (*RoomsSessionsID, error) {
	roomsSessionsID := RoomsSessionsID{
		roomIDuserIDdeviceID: make(map[string]map[string]map[string]*SessionsID),
	}
	err := cdb.db.View(func(tx *bolt.Tx) error {
		roomBucket := tx.Bucket([]byte("crypto_rooms")).Bucket([]byte(roomID))
		err := roomBucket.ForEach(func(userID, v []byte) error {
			userBucket := roomBucket.Bucket(userID)
			err := userBucket.ForEach(func(deviceID, v []byte) error {
				deviceBucket := userBucket.Bucket(deviceID)
				olmSessionID := deviceBucket.Get([]byte("olm_session_id"))
				if olmSessionID != nil {
					roomsSessionsID.SetOlmSessionID(roomID, string(userID),
						string(deviceID), string(olmSessionID))
				}
				return nil
			})
			return err
		})
		return err
	})
	return &roomsSessionsID, err
}

func SplitAlgorithmKeyID(algorithmKeyID string) (string, string) {
	algorithmKeyIDSlice := strings.Split(algorithmKeyID, ":")
	if len(algorithmKeyIDSlice) != 2 {
		return "", ""
	}
	return algorithmKeyIDSlice[0], algorithmKeyIDSlice[1]
}

func main() {
	myUser := &MyUser{
		ID: userID,
		Device: &MyDevice{
			ID: deviceID,
		},
	}

	db, err := OpenCryptoDB("test.db")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	if !db.ExistsUserDevice(myUser.ID, myUser.Device.ID) {
		db.AddMyUserMyDevice(myUser.ID, myUser.Device.ID)
	}
	if !db.ExistsOlmAccount(myUser.ID, myUser.Device.ID) {
		myUser.Device.OlmAccount = olm.NewAccount()
		myUser.Device.Ed25519, myUser.Device.Curve25519 =
			myUser.Device.OlmAccount.IdentityKeysEd25519Curve25519()
		db.StoreOlmAccount(myUser.ID, myUser.Device.ID, myUser.Device.OlmAccount)
	} else {
		err := db.LoadMyUser(myUser)
		if err != nil {
			panic(err)
		}
	}
	fmt.Println("Identity keys:", myUser.Device.Ed25519, myUser.Device.Curve25519)

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
	myUser.ID = res.UserID

	roomUsers := make(map[string][]string)
	joinedRooms, err := cli.JoinedRooms()
	if err != nil {
		panic(err)
	}
	for i := 0; i < len(joinedRooms.JoinedRooms); i++ {
		roomID := joinedRooms.JoinedRooms[i]
		joinedMembers, err := cli.JoinedMembers(roomID)
		if err != nil {
			panic(err)
		}
		if len(joinedMembers.Joined) < 2 {
			continue
		}
		fmt.Printf("%02d %s\n", i, roomID)
		count := 0
		usersID := []string{}
		for userID, userDetails := range joinedMembers.Joined {
			if userID == myUser.ID {
				continue
			}
			usersID = append(usersID, userID)
			dispName := userDetails.DisplayName
			count++
			if count == 6 && len(joinedMembers.Joined) > 6 {
				fmt.Printf("\t...\n")
				continue
			} else if count > 6 {
				continue
			}
			if dispName != nil {
				fmt.Printf("\t%s (%s)\n", *dispName, userID)
			} else {
				fmt.Printf("\t%s\n", userID)
			}
		}
		roomUsers[roomID] = usersID
		fmt.Printf("\n")
	}

	theirUser := &User{
		//ID:      "@dhole:matrix.org",
		Devices: make(map[string]*Device),
	}

	fmt.Printf("Select room number or press enter for new room: ")
	var input string
	fmt.Scanln(&input)
	roomIdx, err := strconv.Atoi(input)
	var roomID string
	if err != nil {
		fmt.Println("Creating new room...")
		fmt.Printf("Write user ID to invite: ")
		fmt.Scanln(&input)
		if input != "" {
			theirUser.ID = input
		}
		resp, err := cli.CreateRoom(&gomatrix.ReqCreateRoom{
			Invite: []string{theirUser.ID},
		})
		if err != nil {
			panic(err)
		}
		roomID = resp.RoomID
	} else {
		roomID = joinedRooms.JoinedRooms[roomIdx]
		fmt.Println("Selected room is", joinedRooms.JoinedRooms[roomIdx])
		theirUser.ID = roomUsers[roomID][0]
	}

	if !db.ExistsUser(theirUser.ID) {
		db.AddUser(theirUser.ID)
	} else {
		db.LoadUser(theirUser)
	}
	if len(theirUser.Devices) == 0 {
		respQuery, err := cli.KeysQuery(map[string][]string{theirUser.ID: []string{}}, -1)
		if err != nil {
			panic(err)
		}
		fmt.Printf("%+v\n", respQuery)
		for theirDeviceID, deviceKeys := range respQuery.DeviceKeys[theirUser.ID] {
			device := &Device{
				ID: theirDeviceID,
			}
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
			db.AddUserDevice(theirUser.ID, theirDeviceID)
			db.StorePubKeys(theirUser.ID, device.ID, device.Ed25519, device.Curve25519)
			theirUser.Devices[device.ID] = device
		}
	}

	roomsSessionsID, err := db.LoadRoomsSessionsID(roomID)
	if err != nil {
		panic(err)
	}

	deviceKeysAlgorithms := map[string]map[string]string{theirUser.ID: map[string]string{}}
	keysToClaim := 0
	for theirDeviceID, _ := range theirUser.Devices {
		if roomsSessionsID.GetOlmSessionID(roomID, theirUser.ID, theirDeviceID) == "" {
			deviceKeysAlgorithms[theirUser.ID][theirDeviceID] = "signed_curve25519"
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
		for theirDeviceID, _ := range deviceKeysAlgorithms[theirUser.ID] {
			algorithmKey, ok := respClaim.OneTimeKeys[theirUser.ID][theirDeviceID]
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
					_, ok := theirUser.Devices[theirDeviceID]
					if ok {
						device := theirUser.Devices[theirDeviceID]
						oneTimeKey = OTK.Key
						olmSession, err := myUser.Device.OlmAccount.NewOutboundSession(device.Curve25519,
							oneTimeKey)
						if err != nil {
							panic(err)
						}
						db.StoreOlmSession(theirUser.ID, device.ID, olmSession)
						db.StoreOlmSessioID(roomID, theirUser.ID, device.ID, olmSession.ID())
						device.OlmSessions[olmSession.ID()] = olmSession
					}
				}
			}
		}
		fmt.Printf("%+v\n", theirUser)
		for _, device := range theirUser.Devices {
			fmt.Printf("%+v\n", *device)
		}
	}

	//cli.SendMessageEvent(roomID, "m.room.message",
	//	gomatrix.TextMessage{MsgType: "m.text", Body: "I'm unencrypted :("})

	text := fmt.Sprint("I'm encrypted :D ~ ", time.Now().Format("2006-01-02 15:04:05"))

	cli.SendStateEvent(roomID, "m.room.encryption", "",
		map[string]string{"algorithm": "m.olm.v1.curve25519-aes-sha2"})

	for _, device := range theirUser.Devices {
		olmSession := device.OlmSessions[roomsSessionsID.GetOlmSessionID(roomID, theirUser.ID, device.ID)]
		payload := map[string]interface{}{
			"type":           "m.room.message",
			"content":        gomatrix.TextMessage{MsgType: "m.text", Body: text},
			"recipient":      theirUser.ID,
			"sender":         myUser.ID,
			"recipient_keys": map[string]string{"ed25519": device.Ed25519},
			"room_id":        roomID}
		payloadJSON, err := json.Marshal(payload)
		if err != nil {
			panic(err)
		}
		fmt.Println(string(payloadJSON))
		encryptMsgType, encryptedMsg := olmSession.Encrypt(string(payloadJSON))
		db.StoreOlmSession(theirUser.ID, device.ID, olmSession)

		cli.SendMessageEvent(roomID, "m.room.encrypted",
			map[string]interface{}{
				"algorithm": "m.olm.v1.curve25519-aes-sha2",
				"ciphertext": map[string]map[string]interface{}{
					device.Curve25519: map[string]interface{}{
						"type": encryptMsgType,
						"body": encryptedMsg,
					},
				},
				"device_id":  myUser.ID,
				"sender_key": myUser.Device.Curve25519,
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
