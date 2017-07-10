package main

import (
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

	olmAccount := olm.NewAccount()
	fmt.Println("Identity keys:", olmAccount.IdentityKeys())

	//olmSession, err := olmAccount.NewOutboundSession(theirIdentityKey, theirOneTimeKey)
	//if err != nil {
	//	panic(err)
	//}

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
