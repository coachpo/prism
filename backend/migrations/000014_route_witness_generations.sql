-- 000014_route_witness_generations
-- Per-profile monotonic generation for the server-side static route-witness
-- analyzer (Model SPEC §4.4.1). The generation advances exactly once per
-- route-affecting mutation transaction (model/access-target/connection/
-- endpoint/referenced-strategy writes) and is the sole route data version:
-- setup readiness projections and the bounded route-witness resolver bind to
-- one immutable analyzer snapshot per generation. Additive only.

CREATE TABLE public.route_witness_generations (
    profile_id integer NOT NULL,
    generation bigint NOT NULL DEFAULT 1,
    updated_at timestamp with time zone NOT NULL,
    CONSTRAINT route_witness_generations_pkey PRIMARY KEY (profile_id),
    CONSTRAINT ck_route_witness_generations_positive CHECK (generation >= 1)
);
