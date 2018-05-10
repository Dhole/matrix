package main

import (
	"fmt"
	mat "github.com/Dhole/gomatrix"
	olm "gitlab.com/dhole/go-olm"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

func txnID() string {
	return "go" + strconv.FormatInt(time.Now().UnixNano(), 10)
}

type Client struct {
	mat.Client
	Crypto *Container
}

func NewClient(homeserverURL, userID, deviceID, accessToken string, db Databaser) (*Client, error) {
	hsURL, err := url.Parse(homeserverURL)
	if err != nil {
		return nil, err
	}
	// By default, use an in-memory store which will never save filter ids / next batch tokens to disk.
	// The client will work with this storer: it just won't remember across restarts.
	// In practice, a database backend should be used.
	store := mat.NewInMemoryStore()
	cryptoContainer, err := LoadContainer(mat.UserID(userID), mat.DeviceID(deviceID), db)
	cli := Client{
		Client: mat.Client{
			AccessToken:   accessToken,
			HomeserverURL: hsURL,
			UserID:        userID,
			Prefix:        "/_matrix/client/r0",
			Syncer:        mat.NewDefaultSyncer(userID, store),
			Store:         store,
		},
		Crypto: cryptoContainer}
	// By default, use the default HTTP client.
	cli.Client.Client = http.DefaultClient

	return &cli, nil
}

// SyncRequest makes an HTTP request according to http://matrix.org/docs/spec/client_server/r0.2.0.html#get-matrix-client-r0-sync
func (cli *Client) SyncRequest(timeout int, since, filterID string, fullState bool, setPresence string) (resp *mat.RespSync, err error) {
	resp, err = cli.Client.SyncRequest(timeout, since, filterID, fullState, setPresence)
	if err != nil {
		err = cli.ProcessSync(resp)
	}
	return
}

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

func (cli *Client) ProcessSync(resp *mat.RespSync) error {
	return nil
}

func (cli *Client) setRoomEncryption(room *Room, encryptionAlg olm.Algorithm) {

}

func (cli *Client) SetRoomEncryption(roomID string, encryptionAlg olm.Algorithm) error {
	room := cli.Crypto.Room(mat.RoomID(roomID))
	if room == nil {
		return fmt.Errorf("Room %s not in the Crypto Container", roomID)
	}
	return room.SetEncryption(encryptionAlg)
}
