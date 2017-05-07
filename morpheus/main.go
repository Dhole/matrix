package morpheus

import (
	"bytes"
	"fmt"
	"github.com/matrix-org/gomatrix"
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

// TODO: Remove this function and get room data from the state returned by the initial sync!
func (c *Client) loadRoomAndData(roomID string) {
	res := NewGenMap()
	c.cli.StateEvent(roomID, "m.room.name", "", &res)
	name := res.StringKey("name")
	c.cli.StateEvent(roomID, "m.room.topic", "", &res)
	topic := res.StringKey("topic")
	c.cli.StateEvent(roomID, "m.room.canonical_alias", "", &res)
	canonicalAlias := res.StringKey("alias")
	c.ConsolePrintf("Adding room (%s) \"%s\" | \"%s\": %s",
		roomID, name, canonicalAlias, topic)
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
		r.AddUserBatch(userID, username, 0, MemJoin)
	}
	//fmt.Println(roomID, "Batch add complete")
	r.AddUserBatchFinish()
	//fmt.Println(roomID, "Batch finish complete")
}

func (c *Client) AddRoom(roomID, name, canonAlias, topic string) (*Room, error) {
	return c.Rs.Add(&c.cfg.UserID, roomID, name, canonAlias, topic)
}

func (c *Client) ConsolePrintf(format string, args ...interface{}) {
	c.Rs.AddConsoleMessage(fmt.Sprintf(format, args...))
}

func (c *Client) ConsolePrint(args ...interface{}) {
	c.Rs.AddConsoleMessage(fmt.Sprint(args...))
}

type Client struct {
	cli      *gomatrix.Client
	cfg      Config
	Rs       Rooms
	DebugBuf *bytes.Buffer
	exit     chan error
}

func NewClient(configName string, configPaths []string) (*Client, error) {
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

	//ui.Init()
	//ui.SetMyDisplayName(c.cfg.DisplayName)
	//ui.SetMyUserID(c.cfg.UserID)

	c.DebugBuf = bytes.NewBufferString("")
	cli, _ := gomatrix.NewClient(c.cfg.Homeserver, "", "")
	c.cli = cli

	c.Rs.consoleRoomID = ConsoleRoomID
	c.Rs.ConsoleDisplayName = ConsoleDisplayName
	c.Rs.ConsoleUserID = ConsoleUserID
	c.Rs = NewRooms()
	r, _ := c.AddRoom(c.Rs.consoleRoomID, "Console", "", "")
	r.AddUser(c.Rs.ConsoleUserID, c.Rs.ConsoleDisplayName, 100, MemJoin)
	c.Rs.ConsoleRoom = c.Rs.ByID[c.Rs.consoleRoomID]
	if c.Rs.ConsoleRoom != c.Rs.R[0] {
		panic("ConsoleRoom is not Rs.R[0]")
	}

	return &c, nil
}

// Set event callbacks
//ui.SetCallSendText(func(roomID, body string) {
//	_, err := cli.SendText(roomID, body)
//	if err != nil {
//		ui.AddConsoleMessage(fmt.Sprint("send:", err))
//		return
//	}
//})
//ui.SetCallJoinRoom(func(roomIDorAlias string) {
//	resJoin, err := cli.JoinRoom(roomIDorAlias, "", nil)
//	if err != nil {
//		ui.AddConsoleMessage(fmt.Sprint("join:", err))
//		return
//	}
//	roomID := resJoin.RoomID
//	AddRoomData(cli, roomID)
//})
//ui.SetCallLeaveRoom(func(roomID string) {
//	_, err := cli.LeaveRoom(roomID)
//	if err != nil {
//		ui.AddConsoleMessage(fmt.Sprint("leave:", err))
//		return
//	}
//	roomName, err := ui.DelRoom(roomID)
//	if err != nil {
//		ui.AddConsoleMessage(fmt.Sprint("leave:", err))
//	} else {
//		ui.AddConsoleMessage(fmt.Sprintf("Left room (%s) \"%s\"",
//			roomID, roomName))
//	}
//})

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
	resSync, err := c.cli.SyncRequest(30000, "", "", false, "online")
	if err != nil {
		return err
	}
	c.ConsolePrint("Initial sync request finished")
	//fmt.Println("Joined rooms...")
	for roomID, roomHist := range resSync.Rooms.Join {
		// TODO: Check return error
		c.loadRoomAndData(roomID)
		//fmt.Println(roomID)
		fmt.Fprintf(c.DebugBuf, "room %s has %d timeline.events",
			roomID, len(roomHist.Timeline.Events))
		r, ok := c.Rs.ByID[roomID]
		if !ok {
			continue
		}
		for _, ev := range roomHist.Timeline.Events {
			body, ok := ev.Body()
			if ok {
				r.Msgs.PushBack(Message{ev.Type, int64(ev.Timestamp),
					ev.Sender, body})
			}
			//else {
			//	ui.AddConsoleMessage(fmt.Sprintf("%+v", ev))
			//}
		}
		// Fetch a few previous messages
		//start := roomHist.Timeline.PrevBatch
		//end := ""
		//count := 0
		//for {
		//	resMessages, err := cli.Messages(roomID, start, end, 'b', 0)
		//	if err != nil {
		//		fmt.Println(err)
		//		break
		//	}
		//	if len(resMessages.Chunk) == 0 {
		//		break
		//	}
		//	for _, ev := range resMessages.Chunk {
		//		body, ok := ev.Body()
		//		if ok {
		//			count++
		//			PushFrontMessage(roomID, ev.Type,
		//				int64(ev.Timestamp/1000), ev.Sender, body)
		//		}
		//	}
		//	if count >= 50 {
		//		break
		//	}
		//	start = resMessages.End
		//}

		// TODO: Populate rooms state
		// NOTE: Don't display state events in the timeline
		//for _, ev := range roomHist.State.Events {
		//}
	}
	// TODO: Populate invited rooms
	//for roomID, roomHist := range resSync.Rooms.Invite {
	// NOTE: Don't display state events in the timeline
	//for _, ev := range roomHist.State.Events {
	//}
	//}
	// TODO: Populate account data
	//for _, ev := range resSync.AccountData.Events {
	//}

	// TODO: Do this only if the already set display name doesn't match the config
	//if c.cfg.DisplayName != "" {
	//	cli.SetDisplayName(c.cfg.DisplayName)
	//}

	syncer := c.cli.Syncer.(*gomatrix.DefaultSyncer)
	syncer.OnEventType("m.room.message", func(ev *gomatrix.Event) {
		//fmt.Println("Message: ", ev)
		msgType, ok := ev.Body()
		if !ok {
			msgType = "?"
		}
		body, ok := ev.Body()
		if !ok {
			body = "???"
		}
		r := c.Rs.ByID[ev.RoomID]
		if r != nil {
			r.Msgs.PushBack(Message{msgType, int64(ev.Timestamp), ev.Sender, body})
		} else {
			fmt.Fprintf(c.DebugBuf,
				"Received message for room %v, which doesn't exist", ev.RoomID)
		}
	})

	c.exit = make(chan error)
	go func() {
		if err := c.cli.Sync(); err != nil {
			c.exit <- fmt.Errorf("Sync() returned %v", err)
		}
	}()
	return <-c.exit
}

// TODO: Actually stop the c.cli.Sync()
func (c *Client) StopSync() {
	c.exit <- nil
}
