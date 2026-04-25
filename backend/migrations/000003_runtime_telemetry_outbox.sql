CREATE TABLE "public"."runtime_telemetry_outbox" (
    "id" BIGSERIAL NOT NULL,
    "profile_id" integer NOT NULL,
    "ingress_request_id" character varying(36) NOT NULL,
    "payload" jsonb NOT NULL,
    "created_at" timestamp with time zone NOT NULL
);

ALTER TABLE ONLY "public"."runtime_telemetry_outbox" ADD CONSTRAINT "runtime_telemetry_outbox_pkey" PRIMARY KEY (id);

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM pg_tables
        WHERE schemaname = 'public' AND tablename = 'profiles'
    ) THEN
        ALTER TABLE ONLY "public"."runtime_telemetry_outbox"
        ADD CONSTRAINT "runtime_telemetry_outbox_profile_id_fkey"
        FOREIGN KEY (profile_id) REFERENCES profiles(id) ON DELETE RESTRICT;
    END IF;
END $$;

CREATE INDEX idx_runtime_telemetry_outbox_created_at ON runtime_telemetry_outbox USING btree (created_at, id);
CREATE INDEX idx_runtime_telemetry_outbox_profile_ingress_request_id ON runtime_telemetry_outbox USING btree (profile_id, ingress_request_id);
