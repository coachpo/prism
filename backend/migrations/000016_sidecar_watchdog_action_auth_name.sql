-- Persist selected auth names for replayable watchdog priority-patch preflights.

ALTER TABLE ONLY "public"."sidecar_watchdog_actions"
ADD COLUMN "auth_name" text;
