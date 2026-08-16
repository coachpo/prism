-- 000019_connection_routing_schedule
-- Routing schedule per Terminal Target. A connection that owns at least one
-- routing window only participates in routing while the request instant falls
-- inside one of its windows, evaluated against the connection's own IANA
-- timezone. Zero window rows means "no time restriction" and is the only
-- encoding of 7x24 availability; a window set whose union covers the whole week
-- is rejected by the write path so that "always available" has exactly one
-- representation.
--
-- routing_schedule_timezone is the Terminal Target's own routing clock. It is
-- unrelated to user_settings.timezone_preference, which only affects timestamp
-- display and Custom input interpretation and never changes routing.
--
-- weekday_mask is a 7-bit ISO weekday bitmap (bit0=Monday .. bit6=Sunday) and
-- names the day on which the window OPENS. end_minute > 1440 means the window
-- runs past local midnight into the following day; the trailing part is never
-- re-encoded on the following day's bit. Boundaries are half-open
-- [start_minute, end_minute) at minute granularity. The span constraint bounds
-- one window to at most 24 hours, so end_minute never actually exceeds 2879.
--
-- ON DELETE CASCADE follows the existing connection-owned child tables
-- (routing_connection_runtime_state and routing_connection_runtime_leases are
-- both CASCADE on connections, 000001:1331/1337). model_access_targets stays
-- RESTRICT (000001:1298) because it references a connection rather than owning
-- part of it.
--
-- Additive only. Existing connections rows land on
-- routing_schedule_timezone = NULL with zero window rows, which is the current
-- behaviour byte for byte.

ALTER TABLE public.connections
    ADD COLUMN routing_schedule_timezone character varying(100);

CREATE TABLE public.connection_routing_windows (
    id            bigint NOT NULL GENERATED ALWAYS AS IDENTITY,
    connection_id integer NOT NULL,
    profile_id    integer NOT NULL,
    weekday_mask  smallint NOT NULL,
    start_minute  smallint NOT NULL,
    end_minute    smallint NOT NULL,
    created_at    timestamp with time zone NOT NULL,
    updated_at    timestamp with time zone NOT NULL,
    CONSTRAINT pk_connection_routing_windows PRIMARY KEY (id),
    CONSTRAINT ck_connection_routing_windows_weekday_mask
        CHECK (weekday_mask BETWEEN 1 AND 127),
    CONSTRAINT ck_connection_routing_windows_start_minute
        CHECK (start_minute BETWEEN 0 AND 1439),
    CONSTRAINT ck_connection_routing_windows_end_minute
        CHECK (end_minute BETWEEN 1 AND 2880),
    CONSTRAINT ck_connection_routing_windows_span
        CHECK (end_minute > start_minute AND end_minute - start_minute <= 1440),
    CONSTRAINT uq_connection_routing_windows_shape
        UNIQUE (connection_id, weekday_mask, start_minute, end_minute),
    CONSTRAINT connection_routing_windows_connection_profile_fkey
        FOREIGN KEY (connection_id, profile_id)
        REFERENCES public.connections(id, profile_id) ON DELETE CASCADE
);

CREATE INDEX idx_connection_routing_windows_profile_connection
    ON public.connection_routing_windows (profile_id, connection_id);
