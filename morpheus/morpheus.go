package morpheus

import (
	"bytes"
	"fmt"
	"github.com/matrix-org/gomatrix"
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
	resMessages, err := c.cli.Messages(r.ID, start, end, 'b', int(num))
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
func (c *Client) loadRoomAndData(roomID string) {
	res := NewGenMap()
	c.cli.StateEvent(roomID, "m.room.name", "", &res)
	name := res.StringKey("name")
	c.cli.StateEvent(roomID, "m.room.topic", "", &res)
	topic := res.StringKey("topic")
	c.cli.StateEvent(roomID, "m.room.canonical_alias", "", &res)
	canonicalAlias := res.StringKey("alias")
	c.ConsolePrintf("Adding room (%s) %s \"%s\": %s",
		roomID, canonicalAlias, name, topic)
	r, _ := c.AddRoom(roomID, name, canonicalAlias, topic)
	resJoinedMem, err := c.cli.JoinedMembers(roomID)
	if err != nil {
		panic(err)
	}
	//fmt.Println(roomID, "Batch add")
	for userID, userData := range resJoinedMem.Joined {
		username := ""
		if userData.DisplayName != nil {
			username = *userData.DisplayName
		}
		r.Users.AddBatch(userID, username, 0, MemJoin)
	}
	//fmt.Println(roomID, "Batch add complete")
	r.Users.AddBatchFinish()
	//fmt.Println(roomID, "Batch finish complete")
}

func (c *Client) GetUserID() string {
	return c.cfg.UserID
}

func (c *Client) GetDisplayName() string {
	return c.cfg.DisplayName
}

func (c *Client) AddRoom(roomID, name, canonAlias, topic string) (*Room, error) {
	r, err := c.Rs.Add(&c.cfg.UserID, roomID, MemJoin)
	r.SetName(name)
	r.SetCanonAlias(canonAlias)
	r.SetTopic(topic)
	return r, err
}

func (c *Client) ConsolePrintf(format string, args ...interface{}) {
	c.Rs.AddConsoleTextMessage(fmt.Sprintf(format, args...))
}

func (c *Client) ConsolePrint(args ...interface{}) {
	c.Rs.AddConsoleTextMessage(fmt.Sprint(args...))
}

type Client struct {
	cli      *gomatrix.Client
	cfg      Config
	Rs       Rooms
	DebugBuf *bytes.Buffer
	minMsgs  uint

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

	c.DebugBuf = bytes.NewBufferString("")
	c.minMsgs = 50
	cli, _ := gomatrix.NewClient(c.cfg.Homeserver, "", "")
	c.cli = cli

	c.Rs = NewRooms(call)
	c.Rs.consoleRoomID = ConsoleRoomID
	c.Rs.ConsoleDisplayName = ConsoleDisplayName
	c.Rs.ConsoleUserID = ConsoleUserID
	r, _ := c.AddRoom(c.Rs.consoleRoomID, "Console", "", "")
	r.HasFirstMsg = true
	r.Users.Add(c.Rs.ConsoleUserID, c.Rs.ConsoleDisplayName, 100, MemJoin)
	r.Users.Add(c.cfg.UserID, c.cfg.DisplayName, 0, MemJoin)
	c.Rs.ConsoleRoom = c.Rs.ByID(c.Rs.consoleRoomID)
	if c.Rs.ConsoleRoom != c.Rs.R[0] {
		panic("ConsoleRoom is not Rs.R[0]")
	}

	return &c, nil
}

// TODO: Handle error, maybe hold message if unsuccesful
func (c *Client) SendText(roomID, body string) {
	if roomID == c.Rs.consoleRoomID || body[0] == '/' {
		c.Rs.ConsoleRoom.PushTextMessage(txnID(), time.Now().Unix()*1000,
			c.cfg.UserID, body)
		body = strings.TrimPrefix(body, "/")
		args := strings.Fields(body)
		if len(args) < 1 {
			return
		}
		c.Rs.call.Cmd(c.Rs.ByID(roomID), args)
	} else {
		_, err := c.cli.SendText(roomID, body)
		if err != nil {
			c.ConsolePrint("send:", err)
			return
		}
	}
}

// TODO: Return error
func (c *Client) JoinRoom(roomIDorAlias string) {
	resJoin, err := c.cli.JoinRoom(roomIDorAlias, "", nil)
	if err != nil {
		c.ConsolePrint("join:", err)
		return
	}
	roomID := resJoin.RoomID
	c.loadRoomAndData(roomID)
	// TODO: Notify UI of new joined room
}

// TODO: Return error
func (c *Client) LeaveRoom(roomID string) {
	_, err := c.cli.LeaveRoom(roomID)
	if err != nil {
		c.ConsolePrint("leave:", err)
		return
	}
	// TODO: Figure out if room datastructure should be removed or not
	//r, err := c.Rs.Del(roomID)
	r := c.Rs.ByID(roomID)
	if r == nil {
		c.ConsolePrint("leave:", roomID, "not found")
		return
	}
	c.ConsolePrintf("Left room (%s) %s", roomID, r.DispName)
	// TODO: Notify UI of left room
}

func (c *Client) Login() error {
	c.ConsolePrintf("Logging in to %s ...", c.cfg.Homeserver)
	res, err := c.cli.Login(&gomatrix.ReqLogin{
		Type:     "m.login.password",
		User:     c.cfg.Username,
		Password: c.cfg.Password,
	})
	if err != nil {
		return err
	}

	//fmt.Println("Token:", res.AccessToken)
	c.ConsolePrintf("Logged in to %s", c.cfg.Homeserver)
	fmt.Fprintf(c.DebugBuf, "AccessToken:\n%s", res.AccessToken)
	c.cli.SetCredentials(res.UserID, res.AccessToken)

	return nil
}

func (c *Client) Sync() error {
	c.ConsolePrint("Doing initial sync request ...")
	//`{"room":{"timeline":{"limit":50}}}`
	res, err := c.cli.SyncRequest(30000, "", "", false, "online")
	if err != nil {
		return err
	}
	c.ConsolePrint("Initial sync request finished")
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
	c.ConsolePrint("Finished loading rooms")
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
		r, _ := c.Rs.Add(&c.cfg.UserID, roomID, MemJoin)
		for _, ev := range roomData.State.Events {
			r.updateState(&ev)
		}
		r.PushToken(roomData.Timeline.PrevBatch)
		for _, ev := range roomData.Timeline.Events {
			r.PushEvent(&ev)
		}
		r.PushToken(res.NextBatch)
	}
	for roomID, roomData := range res.Rooms.Invite {
		r, _ := c.Rs.Add(&c.cfg.UserID, roomID, MemInvite)
		for _, ev := range roomData.State.Events {
			r.updateState(&ev)
		}
	}
	for roomID, roomData := range res.Rooms.Leave {
		r, _ := c.Rs.Add(&c.cfg.UserID, roomID, MemLeave)
		for _, ev := range roomData.State.Events {
			r.updateState(&ev)
		}
		r.PushToken(roomData.Timeline.PrevBatch)
		for _, ev := range roomData.Timeline.Events {
			r.PushEvent(&ev)
		}
		r.PushToken(res.NextBatch)
	}
}

// TODO: Actually stop the c.cli.Sync()
func (c *Client) StopSync() {
	c.exit <- nil
}

func (c *Client) SetMinMsgs(n uint) {
	c.minMsgs = n
}
