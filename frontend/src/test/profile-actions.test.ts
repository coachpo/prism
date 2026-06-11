import { describe, expect, it } from "vitest"
import { createProfileActions } from "@/context/profile/actions"
import type { Profile } from "@/lib/types"

const timestamp = "2026-04-28T12:00:00Z"

function createProfile(id: number, overrides: Partial<Profile> = {}): Profile {
  return {
    id,
    name: `Profile ${id}`,
    description: null,
    is_active: false,
    is_default: false,
    is_editable: true,
    version: 1,
    created_at: timestamp,
    deleted_at: null,
    updated_at: timestamp,
    ...overrides,
  }
}

function createActionHarness(initialProfiles: Profile[]) {
  let profiles = initialProfiles
  let activeProfile = profiles.find((profile) => profile.is_active) ?? null
  let selectedProfileId: number | null = profiles[0]?.id ?? null
  const activationPayloads: Array<{ id: number; expected_active_profile_id: number }> = []
  const syncCalls: Array<{ activeProfileId: number | null; profileIds: number[] }> = []

  const actions = createProfileActions({
    profilesApi: {
      list: async () => profiles,
      create: async (data) => createProfile(99, { name: data.name, description: data.description ?? null }),
      update: async (id, data) => createProfile(id, data),
      activate: async (id, payload) => {
        activationPayloads.push({ id, expected_active_profile_id: payload.expected_active_profile_id })
        return createProfile(id, { is_active: true })
      },
      delete: async () => undefined,
    },
    getActiveProfile: () => activeProfile,
    getProfiles: () => profiles,
    getSelectedProfileId: () => selectedProfileId,
    setProfiles: (nextProfiles) => {
      profiles = nextProfiles
    },
    setActiveProfile: (nextActiveProfile) => {
      activeProfile = nextActiveProfile
    },
    syncSelectedProfile: (nextProfiles, nextActiveProfileId) => {
      syncCalls.push({
        activeProfileId: nextActiveProfileId,
        profileIds: nextProfiles.map((profile) => profile.id),
      })
      selectedProfileId = nextProfiles[0]?.id ?? null
      return selectedProfileId
    },
  })

  return {
    actions,
    activationPayloads,
    get activeProfile() {
      return activeProfile
    },
    setProfiles: (nextProfiles: Profile[]) => {
      profiles = nextProfiles
    },
    syncCalls,
  }
}

describe("profile action contracts", () => {
  it("activates profiles with the current expected active profile id", async () => {
    const harness = createActionHarness([
      createProfile(1, { is_active: true }),
      createProfile(2),
    ])

    harness.setProfiles([
      createProfile(1),
      createProfile(2, { is_active: true }),
    ])

    await expect(harness.actions.activateProfile(2)).resolves.toMatchObject({ id: 2 })

    expect(harness.activationPayloads).toEqual([
      { id: 2, expected_active_profile_id: 1 },
    ])
    expect(harness.activeProfile?.id).toBe(2)
    expect(harness.syncCalls.at(-1)).toEqual({ activeProfileId: 2, profileIds: [1, 2] })
  })

  it("refreshes profile state and rejects stale active profile conflicts", async () => {
    const conflict = Object.assign(new Error("active profile changed elsewhere"), { status: 409 })
    let profiles = [createProfile(1, { is_active: true }), createProfile(2)]
    let activeProfile: Profile | null = profiles[0]
    const syncCalls: Array<{ activeProfileId: number | null; profileIds: number[] }> = []

    const actions = createProfileActions({
      profilesApi: {
        list: async () => profiles,
        create: async () => createProfile(99),
        update: async (id) => createProfile(id),
        activate: async () => {
          profiles = [createProfile(1), createProfile(2), createProfile(3, { is_active: true })]
          throw conflict
        },
        delete: async () => undefined,
      },
      getActiveProfile: () => activeProfile,
      getProfiles: () => profiles,
      getSelectedProfileId: () => 2,
      setProfiles: (nextProfiles) => {
        profiles = nextProfiles
      },
      setActiveProfile: (nextActiveProfile) => {
        activeProfile = nextActiveProfile
      },
      syncSelectedProfile: (nextProfiles, nextActiveProfileId) => {
        syncCalls.push({
          activeProfileId: nextActiveProfileId,
          profileIds: nextProfiles.map((profile) => profile.id),
        })
        return 2
      },
    })

    await expect(actions.activateProfile(2)).rejects.toBe(conflict)

    expect(activeProfile?.id).toBe(3)
    expect(syncCalls.at(-1)).toEqual({ activeProfileId: 3, profileIds: [1, 2, 3] })
  })
  it("resolves selected profile fallback after deleting the selected profile", async () => {
    const harness = createActionHarness([
      createProfile(1, { is_active: true }),
      createProfile(2),
      createProfile(3, { is_default: true }),
    ])

    harness.setProfiles([
      createProfile(1, { is_active: true }),
      createProfile(3, { is_default: true }),
    ])

    await harness.actions.deleteProfile(1)

    expect(harness.syncCalls.at(0)).toEqual({ activeProfileId: null, profileIds: [3] })
  })
})
