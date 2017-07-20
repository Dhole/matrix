package main

import (
	"encoding/json"
	"fmt"
	"github.com/matrix-org/gomatrix"
	"github.com/mitchellh/mapstructure"
	olm "gitlab.com/dhole/go-olm"
	"strings"
)

var username = "ray_test"
var homeserver = "https://matrix.org"
var password = "CiIYIrD3OtSuudJB"
var deviceID = "5un3HpnWE"
var deviceDisplayName = "go-olm-dev"

type Device struct {
	ID          string
	SigningKey  string
	IdentityKey string
	OneTimeKey  string
}

type User struct {
	ID      string
	Devices map[string]*Device
}

type SignedKey struct {
	Key        string                       `json:"key"`
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

	olmAccount := olm.NewAccount()
	fmt.Println("Identity keys:", olmAccount.IdentityKeys())

	var theirUser User
	theirUser.ID = "@dhole:matrix.org"
	theirUser.Devices = make(map[string]*Device)

	respQuery, err := cli.KeysQuery(map[string][]string{theirUser.ID: []string{}}, -1)
	if err != nil {
		panic(err)
	}
	fmt.Printf("%+v\n", respQuery)

	for theirDeviceID, device := range respQuery.DeviceKeys[theirUser.ID] {
		theirDevice := &Device{
			ID: theirDeviceID,
		}
		for algorithmKeyID, key := range device.Keys {
			algorithm, _ := SplitAlgorithmKeyID(algorithmKeyID)
			switch algorithm {
			case "curve25519":
				theirDevice.IdentityKey = key
			case "ed25519":
				theirDevice.SigningKey = key
			}
		}
		theirUser.Devices[theirDeviceID] = theirDevice
	}

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
					device.OneTimeKey = OTK.Key
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

	identityKeysJSON := olmAccount.IdentityKeys()
	identityKeys := map[string]string{}
	err = json.Unmarshal([]byte(identityKeysJSON), &identityKeys)
	if err != nil {
		panic(err)
	}

	text := "I'm encrypted :D"

	for deviceID, device := range theirUser.Devices {
		olmSession, err := olmAccount.NewOutboundSession(device.IdentityKey,
			device.OneTimeKey)
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

		cli.SendStateEvent(roomID, "m.room.encryption", "",
			map[string]string{"algorithm": "m.olm.v1.curve25519-aes-sha2"})

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
