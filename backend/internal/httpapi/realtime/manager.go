package realtime

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

const (
	analyticsChannel           = "analytics"
	analyticsPresetScopePrefix = "preset="
)

type roomKey struct {
	ProfileID int
	Channel   string
	Scope     string
}

type RealtimeConnection struct {
	id            string
	socket        *websocket.Conn
	writeMu       sync.Mutex
	writeTimeout  time.Duration
	profileID     *int
	channels      map[roomKey]struct{}
	authenticated bool
}

func (c *RealtimeConnection) SendJSON(payload any) bool {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if c.writeTimeout > 0 {
		if err := c.socket.SetWriteDeadline(time.Now().Add(c.writeTimeout)); err != nil {
			return false
		}
		defer func() {
			_ = c.socket.SetWriteDeadline(time.Time{})
		}()
	}
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
	nextID       atomic.Uint64
	mu           sync.RWMutex
	connections  map[string]*RealtimeConnection
	rooms        map[roomKey]map[string]struct{}
	writeTimeout time.Duration
	closed       bool
}

func NewConnectionManager(writeTimeout time.Duration) *ConnectionManager {
	return &ConnectionManager{
		connections:  map[string]*RealtimeConnection{},
		rooms:        map[roomKey]map[string]struct{}{},
		writeTimeout: writeTimeout,
	}
}

func (m *ConnectionManager) Connect(socket *websocket.Conn) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		_ = socket.Close()
		return ""
	}
	connectionID := fmt.Sprintf("rt-%d", m.nextID.Add(1))
	connection := &RealtimeConnection{
		id:           connectionID,
		socket:       socket,
		writeTimeout: m.writeTimeout,
		channels:     map[roomKey]struct{}{},
	}
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

func (m *ConnectionManager) Subscribe(connectionID string, profileID int, channel string, preset ...string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	connection := m.connections[connectionID]
	if connection == nil {
		return false
	}
	if connection.profileID != nil && *connection.profileID != profileID {
		m.clearConnectionSubscriptionsLocked(connection)
	}

	key := subscriptionRoomKey(profileID, channel, firstPreset(preset))
	connection.profileID = intPtr(profileID)
	connection.channels[key] = struct{}{}
	if m.rooms[key] == nil {
		m.rooms[key] = map[string]struct{}{}
	}
	m.rooms[key][connectionID] = struct{}{}
	return true
}

func (m *ConnectionManager) UnsubscribeChannel(connectionID string, channel string, preset ...string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	connection := m.connections[connectionID]
	if connection == nil || connection.profileID == nil {
		return false
	}
	key := subscriptionRoomKey(*connection.profileID, channel, firstPreset(preset))
	if _, ok := connection.channels[key]; !ok {
		return false
	}
	m.removeChannelSubscriptionLocked(connection, key)
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

func (m *ConnectionManager) HasSubscribers(profileID int, channel string, preset ...string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.rooms[subscriptionRoomKey(profileID, channel, firstPreset(preset))]) > 0
}

func (m *ConnectionManager) BroadcastToProfile(profileID int, channel string, payload any, preset ...string) int {
	key := subscriptionRoomKey(profileID, channel, firstPreset(preset))

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

func (m *ConnectionManager) ActiveScopes(profileID int, channel string) []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	scopesByValue := map[string]struct{}{}
	for key, members := range m.rooms {
		if key.ProfileID != profileID || key.Channel != strings.TrimSpace(channel) || len(members) == 0 {
			continue
		}
		scope := activeScopeValue(key)
		if key.Channel == analyticsChannel && scope == "" {
			continue
		}
		scopesByValue[scope] = struct{}{}
	}
	scopes := make([]string, 0, len(scopesByValue))
	for scope := range scopesByValue {
		scopes = append(scopes, scope)
	}
	sort.Strings(scopes)
	return scopes
}

func (m *ConnectionManager) ActiveProfileIDs(channel string) []int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	profileIDsByValue := map[int]struct{}{}
	for key, members := range m.rooms {
		if key.Channel != strings.TrimSpace(channel) || len(members) == 0 {
			continue
		}
		profileIDsByValue[key.ProfileID] = struct{}{}
	}
	profileIDs := make([]int, 0, len(profileIDsByValue))
	for profileID := range profileIDsByValue {
		profileIDs = append(profileIDs, profileID)
	}
	sort.Ints(profileIDs)
	return profileIDs
}

func (m *ConnectionManager) Stats() map[string]any {
	m.mu.RLock()
	defer m.mu.RUnlock()

	rooms := map[string]int{}
	for key, members := range m.rooms {
		rooms[statsRoomName(key)] = len(members)
	}
	return map[string]any{
		"total_connections": len(m.connections),
		"total_rooms":       len(m.rooms),
		"rooms":             rooms,
	}
}

func (m *ConnectionManager) Close() {
	if m == nil {
		return
	}
	m.mu.Lock()
	connections := make([]*RealtimeConnection, 0, len(m.connections))
	for _, connection := range m.connections {
		connections = append(connections, connection)
	}
	m.connections = map[string]*RealtimeConnection{}
	m.rooms = map[roomKey]map[string]struct{}{}
	m.closed = true
	m.mu.Unlock()
	for _, connection := range connections {
		connection.closeWithCode(websocket.CloseGoingAway)
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
	keys := make([]roomKey, 0, len(connection.channels))
	for key := range connection.channels {
		keys = append(keys, key)
	}
	for _, key := range keys {
		m.removeChannelSubscriptionLocked(connection, key)
	}
	connection.channels = map[roomKey]struct{}{}
	connection.profileID = nil
}

func (m *ConnectionManager) removeChannelSubscriptionLocked(connection *RealtimeConnection, key roomKey) {
	delete(connection.channels, key)
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

func subscriptionRoomKey(profileID int, channel string, preset string) roomKey {
	trimmedChannel := strings.TrimSpace(channel)
	return roomKey{
		ProfileID: profileID,
		Channel:   trimmedChannel,
		Scope:     subscriptionScope(trimmedChannel, preset),
	}
}

func subscriptionScope(channel string, preset string) string {
	if strings.TrimSpace(channel) != analyticsChannel {
		return ""
	}
	trimmedPreset := strings.TrimSpace(preset)
	if trimmedPreset == "" {
		return ""
	}
	return analyticsPresetScopePrefix + trimmedPreset
}

func activeScopeValue(key roomKey) string {
	if key.Channel == analyticsChannel {
		return strings.TrimPrefix(key.Scope, analyticsPresetScopePrefix)
	}
	return key.Scope
}

func statsRoomName(key roomKey) string {
	name := fmt.Sprintf("profile_%d_%s", key.ProfileID, key.Channel)
	if key.Scope != "" {
		name += "_" + strings.ReplaceAll(key.Scope, "=", "_")
	}
	return name
}

func firstPreset(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func intPtr(value int) *int {
	return &value
}
