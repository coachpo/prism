package realtime

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

type roomKey struct {
	ProfileID int
	Channel   string
}

type RealtimeConnection struct {
	id            string
	socket        *websocket.Conn
	writeMu       sync.Mutex
	profileID     *int
	channels      map[string]struct{}
	authenticated bool
}

func (c *RealtimeConnection) SendJSON(payload any) bool {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.socket.WriteJSON(payload) == nil
}

func (c *RealtimeConnection) closeWithCode(code int) {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	deadline := time.Now().Add(time.Second)
	_ = c.socket.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(code, ""), deadline)
	_ = c.socket.Close()
}

type ConnectionManager struct {
	nextID      atomic.Uint64
	mu          sync.RWMutex
	connections map[string]*RealtimeConnection
	rooms       map[roomKey]map[string]struct{}
}

func NewConnectionManager() *ConnectionManager {
	return &ConnectionManager{
		connections: map[string]*RealtimeConnection{},
		rooms:       map[roomKey]map[string]struct{}{},
	}
}

func (m *ConnectionManager) Connect(socket *websocket.Conn) string {
	connectionID := fmt.Sprintf("rt-%d", m.nextID.Add(1))
	connection := &RealtimeConnection{
		id:       connectionID,
		socket:   socket,
		channels: map[string]struct{}{},
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.connections[connectionID] = connection
	return connectionID
}

func (m *ConnectionManager) Disconnect(connectionID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.dropConnectionLocked(connectionID)
}

func (m *ConnectionManager) GetConnection(connectionID string) *RealtimeConnection {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.connections[connectionID]
}

func (m *ConnectionManager) Subscribe(connectionID string, profileID int, channel string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	connection := m.connections[connectionID]
	if connection == nil {
		return false
	}
	if connection.profileID != nil && *connection.profileID != profileID {
		m.clearConnectionSubscriptionsLocked(connection)
	}

	connection.profileID = intPtr(profileID)
	connection.channels[channel] = struct{}{}
	key := roomKey{ProfileID: profileID, Channel: channel}
	if m.rooms[key] == nil {
		m.rooms[key] = map[string]struct{}{}
	}
	m.rooms[key][connectionID] = struct{}{}
	return true
}

func (m *ConnectionManager) UnsubscribeChannel(connectionID string, channel string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	connection := m.connections[connectionID]
	if connection == nil || connection.profileID == nil {
		return false
	}
	if _, ok := connection.channels[channel]; !ok {
		return false
	}
	m.removeChannelSubscriptionLocked(connection, channel)
	return true
}

func (m *ConnectionManager) Unsubscribe(connectionID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	connection := m.connections[connectionID]
	if connection == nil {
		return false
	}
	m.clearConnectionSubscriptionsLocked(connection)
	return true
}

func (m *ConnectionManager) HasSubscribers(profileID int, channel string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.rooms[roomKey{ProfileID: profileID, Channel: channel}]) > 0
}

func (m *ConnectionManager) BroadcastToProfile(profileID int, channel string, payload any) int {
	key := roomKey{ProfileID: profileID, Channel: channel}

	m.mu.RLock()
	connectionIDs := make([]string, 0, len(m.rooms[key]))
	connections := make([]*RealtimeConnection, 0, len(m.rooms[key]))
	staleConnectionIDs := make([]string, 0)
	for connectionID := range m.rooms[key] {
		connection := m.connections[connectionID]
		if connection == nil {
			staleConnectionIDs = append(staleConnectionIDs, connectionID)
			continue
		}
		connectionIDs = append(connectionIDs, connectionID)
		connections = append(connections, connection)
	}
	m.mu.RUnlock()

	delivered := 0
	failedConnectionIDs := append([]string(nil), staleConnectionIDs...)
	for index, connection := range connections {
		if connection.SendJSON(payload) {
			delivered++
			continue
		}
		failedConnectionIDs = append(failedConnectionIDs, connectionIDs[index])
	}
	if len(failedConnectionIDs) > 0 {
		m.dropConnections(failedConnectionIDs)
	}
	return delivered
}

func (m *ConnectionManager) Stats() map[string]any {
	m.mu.RLock()
	defer m.mu.RUnlock()

	rooms := map[string]int{}
	for key, members := range m.rooms {
		rooms[fmt.Sprintf("profile_%d_%s", key.ProfileID, key.Channel)] = len(members)
	}
	return map[string]any{
		"total_connections": len(m.connections),
		"total_rooms":       len(m.rooms),
		"rooms":             rooms,
	}
}

func (m *ConnectionManager) dropConnections(connectionIDs []string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, connectionID := range connectionIDs {
		m.dropConnectionLocked(connectionID)
	}
}

func (m *ConnectionManager) dropConnectionLocked(connectionID string) {
	connection := m.connections[connectionID]
	if connection != nil {
		m.clearConnectionSubscriptionsLocked(connection)
		delete(m.connections, connectionID)
		return
	}
	for key, members := range m.rooms {
		delete(members, connectionID)
		if len(members) == 0 {
			delete(m.rooms, key)
		}
	}
}

func (m *ConnectionManager) clearConnectionSubscriptionsLocked(connection *RealtimeConnection) {
	for channel := range connection.channels {
		m.removeChannelSubscriptionLocked(connection, channel)
	}
	connection.channels = map[string]struct{}{}
	connection.profileID = nil
}

func (m *ConnectionManager) removeChannelSubscriptionLocked(connection *RealtimeConnection, channel string) {
	if connection.profileID == nil {
		return
	}
	delete(connection.channels, channel)
	key := roomKey{ProfileID: *connection.profileID, Channel: channel}
	if members := m.rooms[key]; members != nil {
		delete(members, connection.id)
		if len(members) == 0 {
			delete(m.rooms, key)
		}
	}
	if len(connection.channels) == 0 {
		connection.profileID = nil
	}
}

func intPtr(value int) *int {
	return &value
}
