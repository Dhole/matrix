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
			r.AddMessage(msgType, ev.ID, int64(ev.Timestamp),
				ev.Sender, ev.Content)
		}
	}
}

func prependRoomEvents(r *Room, events []gomatrix.Event) {
	for _, ev := range events {
		if msgType, ok := ev.MessageType(); ok {
			r.PushFrontMessage(msgType, ev.ID, int64(ev.Timestamp),
				ev.Sender, ev.Content)
		}
	}
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
	return c.Rs.Add(&c.cfg.UserID, roomID, name, canonAlias, topic)
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

	//ui.Init()
	//ui.SetMyDisplayName(c.cfg.DisplayName)
	//ui.SetMyUserID(c.cfg.UserID)

	c.DebugBuf = bytes.NewBufferString("")
	c.minMsgs = 50
	cli, _ := gomatrix.NewClient(c.cfg.Homeserver, "", "")
	c.cli = cli

	c.Rs = NewRooms(call)
	c.Rs.consoleRoomID = ConsoleRoomID
	c.Rs.ConsoleDisplayName = ConsoleDisplayName
	c.Rs.ConsoleUserID = ConsoleUserID
	r, _ := c.AddRoom(c.Rs.consoleRoomID, "Console", "", "")
	r.Users.Add(c.Rs.ConsoleUserID, c.Rs.ConsoleDisplayName, 100, MemJoin)
	r.Users.Add(c.cfg.UserID, c.cfg.DisplayName, 0, MemJoin)
	c.Rs.ConsoleRoom = c.Rs.ByID[c.Rs.consoleRoomID]
	if c.Rs.ConsoleRoom != c.Rs.R[0] {
		panic("ConsoleRoom is not Rs.R[0]")
	}

	return &c, nil
}

// TODO: Handle error, maybe hold message if unsuccesful
func (c *Client) SendText(roomID, body string) {
	if roomID == c.Rs.consoleRoomID || body[0] == '/' {
		c.Rs.ConsoleRoom.AddTextMessage("1", time.Now().Unix()*1000,
			c.cfg.UserID, body)
		body = strings.TrimPrefix(body, "/")
		args := strings.Fields(body)
		if len(args) < 1 {
			return
		}
		c.Rs.call.Cmd(c.Rs.ByID[roomID], args)
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
	r, err := c.Rs.Del(roomID)
	if err != nil {
		c.ConsolePrint("leave:", err)
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
		appendRoomEvents(r, roomHist.Timeline.Events)
		// Fetch a few previous messages
		start := roomHist.Timeline.PrevBatch
		end := ""
		for {
			resMessages, err := c.cli.Messages(roomID, start, end, 'b', 0)
			if err != nil {
				fmt.Println(err)
				break
			}
			if len(resMessages.Chunk) == 0 {
				break
			}
			prependRoomEvents(r, resMessages.Chunk)
			if uint(r.Msgs.Len()) >= c.minMsgs {
				break
			}
			start = resMessages.End
		}

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
		r := c.Rs.ByID[ev.RoomID]
		if r != nil {
			if ev.Type == "m.room.message" {
				msgType, ok := ev.Content["msgtype"].(string)
				if !ok {
					return
				}
				r.AddMessage(msgType, ev.ID, int64(ev.Timestamp),
					ev.Sender, ev.Content)
				//c.RecvMsgsChan <- MessageRoom{m, r}
			}
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

func (c *Client) SetMinMsgs(n uint) {
	c.minMsgs = n
}