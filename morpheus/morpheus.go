package morpheus

import (
	"bytes"
	"fmt"
	"github.com/matrix-org/gomatrix"
	"sync"
	"time"
	//"github.com/pkg/profile"
	"github.com/spf13/viper"
	"strings"
)

type Config struct {
	Username    string
	UserID      string
	DisplayName string
	Password    string
	Homeserver  string
}

type GenMap map[string]interface{}

func NewGenMap() GenMap {
	return GenMap(make(map[string]interface{}))
}

func (m *GenMap) StringKey(k string) string {
	vg, ok := (*m)[k]
	if !ok {
		return ""
	}
	v, ok := vg.(string)
	if !ok {
		return ""
	}
	return v
}

func appendRoomEvents(r *Room, events []gomatrix.Event) {
	for _, ev := range events {
		if msgType, ok := ev.MessageType(); ok {
			r.PushMessage(msgType, ev.ID, int64(ev.Timestamp),
				ev.Sender, ev.Content)
		}
	}
}

func prependRoomEvents(r *Room, events []gomatrix.Event) uint {
	count := uint(0)
	for _, ev := range events {
		if msgType, ok := ev.MessageType(); ok {
			if err := r.PushFrontMessage(msgType, ev.ID, int64(ev.Timestamp),
				ev.Sender, ev.Content); err == nil {
				count++
			}
		}
	}
	return count
}

func (c *Client) GetPrevEvents(r *Room, num uint) (uint, error) {
	r.ExpBackoff.Wait()
	count := uint(0)
	if r.Events.Len() == 0 {
		r.ExpBackoff.Inc()
		return 0, fmt.Errorf("Events is empty")
	}
	token, ok := r.Events.Front().Value.(Token)
	if !ok {
		r.ExpBackoff.Inc()
		return 0, fmt.Errorf("Top Event is not a token")
	}
	start := string(token)
	end := ""
	resMessages, err := c.cli.Messages(r.ID(), start, end, 'b', int(num))
	if err != nil {
		r.ExpBackoff.Inc()
		return 0, err
	}
	r.ExpBackoff.Reset()
	if len(resMessages.Chunk) < int(num) {
		r.HasFirstMsg = true
		if len(resMessages.Chunk) == 0 {
			return 0, nil
		}
	}
	count += prependRoomEvents(r, resMessages.Chunk)
	r.PushFrontToken(resMessages.End)
	return count, nil
}

// TODO: Remove this function and get room data from the state returned by the initial sync!
//func (c *Client) loadRoomAndData(roomID string) {
//	res := NewGenMap()
//	c.cli.StateEvent(roomID, "m.room.name", "", &res)
//	name := res.StringKey("name")
//	c.cli.StateEvent(roomID, "m.room.topic", "", &res)
//	topic := res.StringKey("topic")
//	c.cli.StateEvent(roomID, "m.room.canonical_alias", "", &res)
//	canonicalAlias := res.StringKey("alias")
//	c.ConsolePrintf("Adding room (%s) %s \"%s\": %s",
//		roomID, canonicalAlias, name, topic)
//	r := c.AddRoom(roomID, name, canonicalAlias, topic)
//	resJoinedMem, err := c.cli.JoinedMembers(roomID)
//	if err != nil {
//		panic(err)
//	}
//	//fmt.Println(roomID, "Batch add")
//	for userID, userData := range resJoinedMem.Joined {
//		username := ""
//		if userData.DisplayName != nil {
//			username = *userData.DisplayName
//		}
//		r.Users.AddUpdate(userID, username, 0, MemJoin)
//	}
//	//fmt.Println(roomID, "Batch add complete")
//	//r.Users.AddBatchFinish()
//	//fmt.Println(roomID, "Batch finish complete")
//}

func (c *Client) GetUserID() string {
	return c.cfg.UserID
}

func (c *Client) GetDisplayName() string {
	return c.cfg.DisplayName
}

func (c *Client) AddRoom(roomID, name, canonAlias, topic string) *Room {
	r := c.Rs.AddUpdate(&c.cfg.UserID, roomID, MemJoin)
	r.SetName(name)
	r.SetCanonAlias(canonAlias)
	r.SetTopic(topic)
	return r
}

func (c *Client) ConsolePrintf(txtType MsgTxtType, format string, args ...interface{}) {
	c.Rs.AddConsoleTextMessage(txtType, fmt.Sprintf(format, args...))
}

func (c *Client) ConsolePrint(txtType MsgTxtType, args ...interface{}) {
	c.Rs.AddConsoleTextMessage(txtType, fmt.Sprint(args...))
}

func (c *Client) DebugPrintf(format string, args ...interface{}) {
	c.debugBufMux.Lock()
	fmt.Fprint(c.debugBuf, "\x1b[38;5;110m", time.Now().Format("15:04:05"), "\x1b[0;0m", " ")
	fmt.Fprintf(c.debugBuf, format, args...)
	fmt.Fprint(c.debugBuf, "\n")
	c.debugBufMux.Unlock()
}

func (c *Client) DebugPrint(args ...interface{}) {
	c.debugBufMux.Lock()
	fmt.Fprint(c.debugBuf, "\x1b[38;5;110m", time.Now().Format("15:04:05"), "\x1b[0;0m", " ")
	fmt.Fprint(c.debugBuf, args...)
	fmt.Fprint(c.debugBuf, "\n")
	c.debugBufMux.Unlock()
}

func (c *Client) DebugBuf() *bytes.Buffer {
	return c.debugBuf
}

type Client struct {
	cli         *gomatrix.Client
	cfg         Config
	Rs          Rooms
	debugBuf    *bytes.Buffer
	debugBufMux sync.Mutex
	//minMsgs     uint

	exit chan error

	//	sentMsgsChan chan MessageRoom
}

func NewClient(configName string, configPaths []string, call Callbacks) (*Client, error) {
	//defer profile.Start().Stop()
	viper.SetConfigType("toml")
	viper.SetConfigName(configName)
	for _, configPath := range configPaths {
		viper.AddConfigPath(configPath)
	}

	if err := viper.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("Error config file: %s \n", err)
	}

	mustExistKeys := []string{"Username", "Password", "Homeserver"}
	for _, key := range mustExistKeys {
		if !viper.IsSet(key) {
			return nil, fmt.Errorf("Key %s not found in config file", key)
		}
	}
	var c Client
	if err := viper.Unmarshal(&c.cfg); err != nil {
		return nil, fmt.Errorf("Error decoding config file, %v", err)
	}

	c.cfg.UserID = fmt.Sprintf("@%s:%s", c.cfg.Username,
		strings.TrimPrefix(c.cfg.Homeserver, "https://"))

	c.debugBuf = bytes.NewBufferString("")
	//c.minMsgs = 50
	cli, _ := gomatrix.NewClient(c.cfg.Homeserver, "", "")
	cli.Prefix = "/_matrix/client/unstable"
	c.cli = cli

	c.Rs = NewRooms(call)
	c.Rs.consoleUserID = ConsoleUserID
	r := c.AddRoom(ConsoleRoomID, "Console", "", "")
	r.HasFirstMsg = true
	r.Users.AddUpdate(c.Rs.consoleUserID, ConsoleUserDisplayName, 100, MemJoin)
	r.Users.AddUpdate(c.cfg.UserID, c.cfg.DisplayName, 0, MemJoin)
	c.Rs.SetConsoleRoom(r)
	if c.Rs.ConsoleRoom() != c.Rs.R[0] {
		panic("ConsoleRoom is not Rs.R[0]")
	}

	return &c, nil
}

// TODO: Handle error, maybe hold message if unsuccesful
func (c *Client) SendText(roomID, body string) {
	if roomID == c.Rs.ConsoleRoom().ID() || body[0] == '/' {
		c.Rs.ConsoleRoom().PushTextMessage(MsgTxtTypeText, txnID(),
			time.Now().Unix()*1000, c.cfg.UserID, body)
		body = strings.TrimPrefix(body, "/")
		args := strings.Fields(body)
		if len(args) < 1 {
			return
		}
		c.Rs.call.Cmd(c.Rs.ByID(roomID), args)
	} else {
		_, err := c.cli.SendText(roomID, body)
		if err != nil {
			c.ConsolePrint(MsgTxtTypeNotice, "send:", err)
			return
		}
	}
}

// TODO: Return error
func (c *Client) JoinRoom(roomIDorAlias string) {
	_, err := c.cli.JoinRoom(roomIDorAlias, "", nil)
	if err != nil {
		c.ConsolePrint(MsgTxtTypeNotice, "join:", err)
		return
	}
	//roomID := resJoin.RoomID
	//c.loadRoomAndData(roomID)
	// TODO: Notify UI of new joined room
}

// TODO: Return error
func (c *Client) LeaveRoom(roomID string) {
	_, err := c.cli.LeaveRoom(roomID)
	if err != nil {
		c.ConsolePrint(MsgTxtTypeNotice, "leave:", err)
		return
	}
	// TODO: Figure out if room datastructure should be removed or not
	//r, err := c.Rs.Del(roomID)
	r := c.Rs.ByID(roomID)
	if r == nil {
		c.ConsolePrint(MsgTxtTypeNotice, "leave:", roomID, "not found")
		return
	}
	c.ConsolePrintf(MsgTxtTypeNotice, "Left room (%s) %s", roomID, r.DispName)
	// TODO: Notify UI of left room
}

func (c *Client) Login() error {
	c.ConsolePrintf(MsgTxtTypeNotice, "Logging in to %s ...", c.cfg.Homeserver)
	res, err := c.cli.Login(&gomatrix.ReqLogin{
		Type:     "m.login.password",
		User:     c.cfg.Username,
		Password: c.cfg.Password,
	})
	if err != nil {
		return err
	}

	//fmt.Println("Token:", res.AccessToken)
	c.ConsolePrintf(MsgTxtTypeNotice, "Logged in to %s", c.cfg.Homeserver)
	c.DebugPrintf("AccessToken:\n%s", res.AccessToken)
	c.cli.SetCredentials(res.UserID, res.AccessToken)

	return nil
}

func (c *Client) Sync() error {
	c.ConsolePrint(MsgTxtTypeNotice, "Doing initial sync request ...")
	//`{"room":{"timeline":{"limit":50}}}`
	res, err := c.cli.SyncRequest(30000, "", "", false, "online")
	if err != nil {
		return err
	}
	c.ConsolePrint(MsgTxtTypeNotice, "Initial sync request finished")
	//fmt.Println("Joined rooms...")
	c.update(res)
	//for roomID, roomHist := range res.Rooms.Join {
	//	// TODO: Check return error
	//	c.loadRoomAndData(roomID)
	//	//fmt.Println(roomID)
	//	fmt.Fprintf(c.DebugBuf, "room %s has %d timeline.events",
	//		roomID, len(roomHist.Timeline.Events))
	//	r := c.Rs.ByID(roomID)
	//	if r == nil {
	//		continue
	//	}
	//	appendRoomEvents(r, roomHist.Timeline.Events)
	//	r.HasLastMsg = true
	//	// Fetch a few previous messages
	//	r.PushFrontToken(roomHist.Timeline.PrevBatch)
	//	// DEBUG
	//	c.GetPrevEvents(r, c.minMsgs)

	//	// TODO: Populate rooms state
	//	// NOTE: Don't display state events in the timeline
	//	//for _, ev := range roomHist.State.Events {
	//	//}
	//}
	c.ConsolePrint(MsgTxtTypeNotice, "Finished loading rooms")
	// TODO: Populate invited rooms
	//for roomID, roomHist := range res.Rooms.Invite {
	// NOTE: Don't display state events in the timeline
	//for _, ev := range roomHist.State.Events {
	//}
	//}
	// TODO: Populate account data
	//for _, ev := range res.AccountData.Events {
	//}

	// TODO: Do this only if the already set display name doesn't match the config
	//if c.cfg.DisplayName != "" {
	//	cli.SetDisplayName(c.cfg.DisplayName)
	//}

	go func() {
		for {
			res, err = c.cli.SyncRequest(30000, res.NextBatch, "", false, "")
			if err != nil {
				// TODO: Add an exponential back-off up to 5 minutes or something.
				time.Sleep(30)
				continue
			}
			c.update(res)
		}
	}()

	c.exit = make(chan error)
	return <-c.exit
}

func (c *Client) update(res *gomatrix.RespSync) {
	for roomID, roomData := range res.Rooms.Join {
		r := c.Rs.AddUpdate(&c.cfg.UserID, roomID, MemJoin)
		for _, ev := range roomData.State.Events {
			r.updateState(&ev)
			//if err != nil && roomID == "!JpNcLQuoaOfdycmQio:matrix.org" {
			//	c.DebugPrintf("%v - %+v", err, ev)
			//}
		}
		r.PushToken(roomData.Timeline.PrevBatch)
		for _, ev := range roomData.Timeline.Events {
			r.PushEvent(&ev)
		}
		r.PushToken(res.NextBatch)
		//if roomID == "!JpNcLQuoaOfdycmQio:matrix.org" {
		//	c.DebugPrintf("%+v", roomData.State)
		//	c.DebugPrintf("%+v", roomData.Timeline)
		//}
	}
	for roomID, roomData := range res.Rooms.Invite {
		r := c.Rs.AddUpdate(&c.cfg.UserID, roomID, MemInvite)
		for _, ev := range roomData.State.Events {
			r.updateState(&ev)
		}
		//if roomID == "!JpNcLQuoaOfdycmQio:matrix.org" {
		c.DebugPrintf("invite %+v", roomData)
		//}
	}
	for roomID, roomData := range res.Rooms.Leave {
		r := c.Rs.AddUpdate(&c.cfg.UserID, roomID, MemLeave)
		for _, ev := range roomData.State.Events {
			r.updateState(&ev)
		}
		r.PushToken(roomData.Timeline.PrevBatch)
		for _, ev := range roomData.Timeline.Events {
			r.PushEvent(&ev)
		}
		r.PushToken(res.NextBatch)
		//if roomID == "!JpNcLQuoaOfdycmQio:matrix.org" {
		//	c.DebugPrintf("leave %+v", roomData)
		//}
	}
}

// TODO: Actually stop the c.cli.Sync()
func (c *Client) StopSync() {
	c.exit <- nil
}

//func (c *Client) SetMinMsgs(n uint) {
//	c.minMsgs = n
//}
