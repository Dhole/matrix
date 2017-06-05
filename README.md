# Implemented

- Handle join, invite and leave rooms
- Update display name
- Switch between rooms with shortcuts
- Request previous messages for a room
- Messages view scrolling
- Show date when day changes
- Draw line on new received messages
- Highlight room for new messages
- Handle UTF-8 properly.

## Events

- `m.room.canonical_alias`
- `m.room.join_rules`
- `m.room.member`
- `m.room.message`
    - `m.text`
    - `m.emote`
    - `m.notice`
- `m.room.name`
- `m.room.topic`

# TODO

- Messages are being kept as they are received from the /sync API.  Delete them when the list of messages in the room gets too big.
    - Except for the case where the user doesn't have the room scrolled to the bottom, in that case, stop storing new messages and query them (through /messages) when the user scrolls down.
- Implement emacs-like shortcuts in the readline
- Add readline history per room
- Add a readline mode to send messages consisting of multiple lines
- Add hooks for new messages, new highlighted messages (play a sound, send a desktop notification...)

- Allow all colors of the UI to be configured through a file (and maybe through commands too)
- Show new messages since last connection after starting the client.
    - Store last seen message of every room persistently.
- Implement thread-safe room and users operations in the UI.
- Implement a way to represent media (images, videos, audios).  Maybe spawn a program to present the file?

## Basic functionalities

- Highlight room and message for mentions

- Change display name
- Start conversation with a user
- Create a new room
- Invite a user to a room
- Manage room power levels

- Registration and management
- Refresh token if it has expired

