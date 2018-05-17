package matrixolm

import (
	"encoding/json"
	"fmt"
	mat "github.com/Dhole/gomatrix"
	olm "gitlab.com/dhole/go-olm"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

type Logger interface {
	Debugf(format string, args ...interface{})
	Errorf(format string, args ...interface{})
}

func txnID() string {
	return "go" + strconv.FormatInt(time.Now().UnixNano(), 10)
}

type Client struct {
	mat.Client
	Crypto *Container
	log    Logger
}

func NewClient(homeserverURL, userID, deviceID, accessToken string, db Databaser,
	log Logger) (*Client, error) {
	hsURL, err := url.Parse(homeserverURL)
	if err != nil {
		return nil, err
	}
	// By default, use an in-memory store which will never save filter ids / next batch tokens to disk.
	// The client will work with this storer: it just won't remember across restarts.
	// In practice, a database backend should be used.
	store := mat.NewInMemoryStore()
	cryptoContainer, err := LoadContainer(mat.UserID(userID), mat.DeviceID(deviceID), db)
	if err != nil {
		return nil, err
	}
	cli := Client{
		Client: mat.Client{
			AccessToken:   accessToken,
			HomeserverURL: hsURL,
			UserID:        userID,
			Prefix:        "/_matrix/client/unstable",
			Syncer:        mat.NewDefaultSyncer(userID, store),
			Store:         store,
		},
		log:    log,
		Crypto: cryptoContainer}
	device := cli.Crypto.me.Device
	cli.log.Debugf("Loaded Device %s with identity keys Ed25519:%s Curve25519:%s",
		device.ID, device.Ed25519, device.Curve25519)
	// By default, use the default HTTP client.
	cli.Client.Client = http.DefaultClient

	// TODO: Check if we've already uploaded device identity keys and
	// one-time keys to server, and if not or if required, upload them using `/keys/upload`

	return &cli, nil
}

type DecryptedEvent struct {
	Error error
	Event mat.Event
}

// Event represents a single Matrix event.
type Event struct {
	mat.Event
	Decrypted *DecryptedEvent
}

// SendToDeviceEvent represents an event received through the send-to-device API:
// https://matrix.org/speculator/spec/drafts%2Fe2e/client_server/unstable.html#extensions-to-sync
type SendToDeviceEvent struct {
	mat.SendToDeviceEvent
	Decrypted *DecryptedEvent
}

// RespSync is the JSON response for http://matrix.org/docs/spec/client_server/r0.2.0.html#get-matrix-client-r0-sync
type RespSync struct {
	NextBatch   string `json:"next_batch"`
	AccountData struct {
		Events []Event `json:"events"`
	} `json:"account_data"`
	Presence struct {
		Events []Event `json:"events"`
	} `json:"presence"`
	Rooms struct {
		Leave map[string]struct {
			State struct {
				Events []Event `json:"events"`
			} `json:"state"`
			Timeline struct {
				Events    []Event `json:"events"`
				Limited   bool    `json:"limited"`
				PrevBatch string  `json:"prev_batch"`
			} `json:"timeline"`
		} `json:"leave"`
		Join map[string]struct {
			State struct {
				Events []Event `json:"events"`
			} `json:"state"`
			Timeline struct {
				Events    []Event `json:"events"`
				Limited   bool    `json:"limited"`
				PrevBatch string  `json:"prev_batch"`
			} `json:"timeline"`
		} `json:"join"`
		Invite map[string]struct {
			State struct {
				Events []Event
			} `json:"invite_state"`
		} `json:"invite"`
	} `json:"rooms"`
	ToDevice struct {
		Events []SendToDeviceEvent `json:"events"`
	} `json:"to_device"`
}

// SyncRequest makes an HTTP request according to http://matrix.org/docs/spec/client_server/r0.2.0.html#get-matrix-client-r0-sync
func (cli *Client) SyncRequest(timeout int, since, filterID string, fullState bool, setPresence string) (resp *RespSync, err error) {
	query := map[string]string{
		"timeout": strconv.Itoa(timeout),
	}
	if since != "" {
		query["since"] = since
	}
	if filterID != "" {
		query["filter"] = filterID
	}
	if setPresence != "" {
		query["set_presence"] = setPresence
	}
	if fullState {
		query["full_state"] = "true"
	}
	urlPath := cli.Client.BuildURLWithQuery([]string{"sync"}, query)
	_, err = cli.Client.MakeRequest("GET", urlPath, nil, &resp)
	if err == nil {
		cli.parseSync(resp)
	}
	return
}

func (cli *Client) parseSync(res *RespSync) {
	// TODO: res.Rooms.Leave
	for roomID, roomData := range res.Rooms.Join {
		room := cli.Crypto.Room(mat.RoomID(roomID))
		for i := range roomData.Timeline.Events {
			ev := &roomData.Timeline.Events[i]
			ev.RoomID = roomID
			err := cli.parseEvent(room, ev)
			if err != nil {
				cli.log.Errorf("error parsing rooms.join.timeline.event for room "+
					"%s: %s", ev.RoomID, err)
			}
			ev.Decrypted = &DecryptedEvent{Error: fmt.Errorf("hola")}
		}
	}
	//for roomID, _ := range res.Rooms.Invite {
	//}
	// TODO?: res.AccountData
	for _, ev := range res.ToDevice.Events {
		sender, body := parseSendToDeviceEvent(&ev)
		cli.log.Debugf("sendToDevice event from %s: %s\n", sender, body)
	}
}

func (cli *Client) parseEvent(room *Room, ev *Event) error {
	switch ev.Type {
	case "m.room.member":
		return cli.parseRoomMember(room, ev)
	case "m.room.encryption":
		return cli.parseRoomEncryption(room, ev)
	case "m.room.encrypted":
		decEv, err := cli.parseRoomEncrypted(room, ev)
		if err != nil {
			return err
		}
		return cli.parseEvent(room, decEv)
	case "m.room_key":
	case "m.room_key_request":
		cli.log.Debugf("m.room_key_request event type not implemented yet")
	case "m.forwarded_room_key":
		cli.log.Debugf("m.forwarded_room_key event type not implemented yet")
	default:
	}
	return nil
}

func (cli *Client) parseRoomMember(room *Room, ev *Event) error {
	var roomMember ContentRoomMember
	err := mapUnmarshal(ev.Content, &roomMember)
	if err != nil {
		return err
	}
	if ev.StateKey == nil {
		return fmt.Errorf("m.room.member event doesn't contain state_key: %+v", ev)
	}
	userID := mat.UserID(*ev.StateKey)
	switch roomMember.Membership {
	case "invite":
		room.SetUserMembership(userID, MemInvite)
	case "join":
		room.SetUserMembership(userID, MemJoin)
	case "leave":
		room.SetUserMembership(userID, MemLeave)
	case "ban":
		room.SetUserMembership(userID, MemBan)
	default:
		return fmt.Errorf("Unknown membership in m.room.member: %s", roomMember.Membership)
	}
	return nil
}

type ContentRoomMember struct {
	DisplayName string `json:"displayname"`
	Membership  string `json:"membership"`
}

func (cli *Client) parseRoomEncryption(room *Room, ev *Event) error {
	var roomMember ContentRoomMember
	err := mapUnmarshal(ev.Content, &roomMember)
	if err != nil {
		return err
	}
	algorithm, ok := ev.Content["algorithm"].(string)
	if !ok {
		return fmt.Errorf("Error parsing m.room.encryption event")
	}
	encryptionAlg := olm.Algorithm(algorithm)
	switch encryptionAlg {
	case olm.AlgorithmOlmV1:
	case olm.AlgorithmMegolmV1:
	default:
		return fmt.Errorf("Unsupported algorithm %s in m.room.encryption event", algorithm)
	}
	return cli.setRoomEncryption(room, encryptionAlg)
}

func (cli *Client) parseRoomEncrypted(room *Room, ev *Event) (*Event, error) {
	var err error
	var decEvJSON string
	ev.Decrypted = &DecryptedEvent{}
	defer func() {
		ev.Decrypted.Error = err
	}()
	switch ev.Content["algorithm"] {
	case string(olm.AlgorithmOlmV1):
		var olmMsg OlmMsg
		err = mapUnmarshal(ev.Content, &olmMsg)
		if err != nil {
			return nil, err
		}
		//senderKey = olmMsg.SenderKey
		decEvJSON, err = cli.decryptOlmMsg(ev, &olmMsg)
	//case string(olm.AlgorithmMegolmV1):
	//	var megolmMsg MegolmMsg
	//	err = mapUnmarshal(content, &megolmMsg)
	//	if err != nil {
	//		return nil, err
	//	}
	//	//senderKey = megolmMsg.SenderKey
	//	decEvJSON, err = decryptMegolmMsg(&megolmMsg, userID, roomID)
	default:
		return nil, fmt.Errorf("Encryption algorithm %+v not supported",
			ev.Content["algorithm"])
	}
	var decEv Event
	err = json.Unmarshal([]byte(decEvJSON), &decEv)
	return &decEv, err
}

func (cli *Client) UserDevices(userID mat.UserID) (*UserDevices, error) {
	userDevices, ok := cli.Crypto.users[userID]
	if ok {
		return userDevices, nil
	} else {
		var userDevices *UserDevices
		userDevices = NewUserDevices(userID)
		cli.Crypto.users[userDevices.id] = userDevices
		cli.Crypto.db.AddUser(userDevices.id)

		if err := cli.updateUserDevices(userDevices); err != nil {
			return nil, err
		}
		return userDevices, nil
	}
}

func (cli *Client) userDevice(ud *UserDevices, deviceKey olm.Curve25519) (*Device, error) {
	device := ud.Devices[deviceKey]
	if device == nil {
		return nil, fmt.Errorf("Device with key %s for user %s not available",
			deviceKey, ud.ID())
	}
	return device, nil
}

// NOTE: Call this after the device keys have been verified to be signed by the
// ed25519 key of the device!
func (cli *Client) addUserDevice(ud *UserDevices, deviceID mat.DeviceID,
	ed25519 olm.Ed25519, curve25519 olm.Curve25519) *Device {
	// TODO!!!
	// We pick the device by curve25519 because
	device := ud.Devices[curve25519]
	// We only add the device if we didn't have it before
	if device == nil {
		device = &Device{
			user:             ud,
			ID:               mat.DeviceID(deviceID),
			Ed25519:          ed25519,
			Curve25519:       curve25519,
			OlmSessions:      make(map[olm.SessionID]*olm.Session),
			MegolmInSessions: make(map[olm.SessionID]*olm.InboundGroupSession),
		}
		ud.Devices[device.Curve25519] = device
		ud.DevicesByID[device.ID] = device
		cli.Crypto.db.AddUserDevice(ud.id, device.ID)
		cli.Crypto.db.StorePubKeys(ud.id, device.ID, device.Ed25519, device.Curve25519)
	}
	return device
}

func (cli *Client) updateUserDevices(ud *UserDevices) error {
	cli.log.Debugf("Updating list of user", ud.id, "devices")
	respQuery, err := cli.Client.KeysQuery(map[string][]string{string(ud.id): []string{}}, -1)
	if err != nil {
		return err
	}
	//fmt.Printf("%+v\n", respQuery)
	// TODO: Verify signatures, and save who has signed the key
	for theirDeviceID, deviceKeys := range respQuery.DeviceKeys[string(ud.id)] {
		var ed25519 olm.Ed25519
		var curve25519 olm.Curve25519
		for algorithmKeyID, key := range deviceKeys.Keys {
			algorithm, theirDeviceID2 := SplitAlgorithmKeyID(algorithmKeyID)
			if theirDeviceID != theirDeviceID2 {
				panic("TODO: Handle this case")
			}
			switch algorithm {
			case "ed25519":
				ed25519 = olm.Ed25519(key)
			case "curve25519":
				curve25519 = olm.Curve25519(key)
			}
		}
		if ed25519 == "" || curve25519 == "" {
			// TODO: Handle this case properly
			continue
		}
		cli.addUserDevice(ud, mat.DeviceID(theirDeviceID), ed25519, curve25519)
	}
	return nil
}

func (cli *Client) storeNewOlmSession(device *Device, roomID mat.RoomID, userID mat.UserID,
	session *olm.Session) {
	cli.Crypto.sessionsID.setOlmSessionID(roomID, userID, device.Curve25519, session.ID())
	cli.Crypto.db.StoreOlmSessionID(roomID, userID, device.Curve25519, session.ID())
	device.OlmSessions[session.ID()] = session
	cli.Crypto.db.StoreOlmSession(userID, device.ID, session)
}

func (cli *Client) decryptOlmMsg(ev *Event, olmMsg *OlmMsg) (string, error) {
	sender := mat.UserID(ev.Sender)
	roomID := mat.RoomID(ev.RoomID)
	if olmMsg.SenderKey == cli.Crypto.me.Device.Curve25519 {
		// TODO: Cache self encrypted olm messages so that they can be queried here
		return "", fmt.Errorf("Olm encrypted messages by myself not cached yet")
	}
	// NOTE: olm messages can be decrypted without the sender keys
	userDevices, err := cli.UserDevices(sender)
	if err != nil {
		return "", err
	}
	device, err := cli.userDevice(userDevices, olmMsg.SenderKey)
	if err != nil {
		return "", err
	}
	ciphertext, ok := olmMsg.Ciphertext[cli.Crypto.me.Device.Curve25519]
	if !ok {
		return "", fmt.Errorf("Message not encrypted for our Curve25519 key %s",
			cli.Crypto.me.Device.Curve25519)
	}
	var session *olm.Session
	sessionsID := cli.Crypto.sessionsID.getSessionsID(roomID, sender, olmMsg.SenderKey)
	if sessionsID == nil {
		// Is this a pre key message where the sender has started an olm session?
		if ciphertext.Type == olm.MsgTypePreKey {
			session, err = cli.Crypto.me.Device.OlmAccount.
				NewInboundSession(ciphertext.Body)
			if err != nil {
				return "", err
			}
			cli.storeNewOlmSession(device, roomID, sender, session)

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
			session2, err2 := cli.Crypto.me.Device.OlmAccount.
				NewInboundSession(ciphertext.Body)
			if err2 != nil {
				return "", err
			}
			msg, err2 = session2.Decrypt(ciphertext.Body, ciphertext.Type)
			if err2 != nil {
				return "", err
			}
			session = session2
			cli.storeNewOlmSession(device, roomID, sender, session)
			return msg, nil
		} else {
			return "", err
		}
	}
	cli.Crypto.db.StoreOlmSession(sender, device.ID, session)
	return msg, nil
}

// "m.room.member": invite, join, leave, ban, knock

// SendMessageEvent sends a message event into a room. See http://matrix.org/docs/spec/client_server/r0.2.0.html#put-matrix-client-r0-rooms-roomid-send-eventtype-txnid
// contentJSON should be a pointer to something that can be encoded as JSON using json.Marshal.
func (cli *Client) SendMessageEvent(roomID string, eventType string, contentJSON interface{}) (resp *mat.RespSendEvent, err error) {
	txnID := txnID()
	urlPath := cli.Client.BuildURL("rooms", roomID, "send", eventType, txnID)
	_, err = cli.Client.MakeRequest("PUT", urlPath, contentJSON, &resp)
	return
}

// SendText sends an m.room.message event into the given room with a msgtype of m.text
// See http://matrix.org/docs/spec/client_server/r0.2.0.html#m-text
func (cli *Client) SendText(roomID, text string) (*mat.RespSendEvent, error) {
	return cli.SendMessageEvent(roomID, "m.room.message",
		mat.TextMessage{MsgType: "m.text", Body: text})
}

// SendImage sends an m.room.message event into the given room with a msgtype of m.image
// See https://matrix.org/docs/spec/client_server/r0.2.0.html#m-image
func (cli *Client) SendImage(roomID, body, url string) (*mat.RespSendEvent, error) {
	return cli.SendMessageEvent(roomID, "m.room.message",
		mat.ImageMessage{
			MsgType: "m.image",
			Body:    body,
			URL:     url,
		})
}

// SendVideo sends an m.room.message event into the given room with a msgtype of m.video
// See https://matrix.org/docs/spec/client_server/r0.2.0.html#m-video
func (cli *Client) SendVideo(roomID, body, url string) (*mat.RespSendEvent, error) {
	return cli.SendMessageEvent(roomID, "m.room.message",
		mat.VideoMessage{
			MsgType: "m.video",
			Body:    body,
			URL:     url,
		})
}

// SendNotice sends an m.room.message event into the given room with a msgtype of m.notice
// See http://matrix.org/docs/spec/client_server/r0.2.0.html#m-notice
func (cli *Client) SendNotice(roomID, text string) (*mat.RespSendEvent, error) {
	return cli.SendMessageEvent(roomID, "m.room.message",
		mat.TextMessage{MsgType: "m.notice", Body: text})
}

//func main() {
//	db, err := OpenCryptoDB("test.db")
//	if err != nil {
//		panic(err)
//	}
//	cli, _ := NewClient("https://matrix.org", "@ray_test:matrix.org", "5un3HpnWE04", "", db)
//	cli.Prefix = "/_matrix/client/unstable"
//	fmt.Printf("%+v\n", cli)
//}

// setRoomEncryption sets the encryption algorithm for the room in the interal state.
func (cli *Client) setRoomEncryption(room *Room, encryptionAlg olm.Algorithm) error {
	if room.encryptionAlg == olm.AlgorithmNone {
		room.encryptionAlg = encryptionAlg
		cli.Crypto.db.StoreRoomEncryptionAlg(room.ID(), encryptionAlg)
	} else if room.encryptionAlg != encryptionAlg {
		return fmt.Errorf("The room %v already has the encryption algorithm %v set",
			room.ID(), room.encryptionAlg)
	}
	return nil
}

// SetRoomEncryption sends the event to set the encryption Algorithm of the
// room and sets it in the internal state.
func (cli *Client) SetRoomEncryption(roomID string, encryptionAlg olm.Algorithm) error {
	room := cli.Crypto.Room(mat.RoomID(roomID))
	if room.encryptionAlg == olm.AlgorithmNone {
		_, err := cli.Client.SendStateEvent(string(room.id), "m.room.encryption", "",
			map[string]string{"algorithm": string(encryptionAlg)})
		if err == nil {
			return cli.setRoomEncryption(room, encryptionAlg)
		}
		return err
	} else {
		return fmt.Errorf("The room %v already has the encryption algorithm %v set",
			room.ID(), room.encryptionAlg)
	}
}
