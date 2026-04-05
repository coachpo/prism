import {
  createReferenceDataStore,
  referenceDataRegistry,
  type ReferenceDataKind,
} from "./referenceDataRegistry";

const sharedReferenceDataStore = createReferenceDataStore(referenceDataRegistry);

type ReferenceDataValue<K extends ReferenceDataKind> = Awaited<
  ReturnType<(typeof referenceDataRegistry)[K]["load"]>
>;

function createSharedReferenceDataAccessors<K extends ReferenceDataKind>(kind: K) {
  return {
    get: (revision: number, forceRefresh = false) =>
      sharedReferenceDataStore.get<K>(kind, revision, forceRefresh),
    set: (revision: number, data: ReferenceDataValue<K>) => {
      sharedReferenceDataStore.set<K>(kind, revision, data);
    },
  };
}

const sharedModels = createSharedReferenceDataAccessors("models");
const sharedVendors = createSharedReferenceDataAccessors("vendors");
const sharedEndpoints = createSharedReferenceDataAccessors("endpoints");
const sharedConnections = createSharedReferenceDataAccessors("connections");
const sharedPricingTemplates = createSharedReferenceDataAccessors("pricingTemplates");
const sharedLoadbalanceStrategies = createSharedReferenceDataAccessors(
  "loadbalanceStrategies",
);

export function clearSharedReferenceData(kind?: ReferenceDataKind, revision?: number) {
  sharedReferenceDataStore.clear(kind, revision);
}

export function getSharedModels(revision: number, forceRefresh = false) {
  return sharedModels.get(revision, forceRefresh);
}

export function setSharedModels(revision: number, data: ReferenceDataValue<"models">) {
  sharedModels.set(revision, data);
}

export function getSharedVendors(revision: number, forceRefresh = false) {
  return sharedVendors.get(revision, forceRefresh);
}

export function setSharedVendors(revision: number, data: ReferenceDataValue<"vendors">) {
  sharedVendors.set(revision, data);
}

export function getSharedEndpoints(revision: number, forceRefresh = false) {
  return sharedEndpoints.get(revision, forceRefresh);
}

export function setSharedEndpoints(revision: number, data: ReferenceDataValue<"endpoints">) {
  sharedEndpoints.set(revision, data);
}

export function getSharedConnectionOptions(revision: number, forceRefresh = false) {
  return sharedConnections.get(revision, forceRefresh);
}

export function setSharedConnectionOptions(
  revision: number,
  data: ReferenceDataValue<"connections">,
) {
  sharedConnections.set(revision, data);
}

export function getSharedPricingTemplates(revision: number, forceRefresh = false) {
  return sharedPricingTemplates.get(revision, forceRefresh);
}

export function setSharedPricingTemplates(
  revision: number,
  data: ReferenceDataValue<"pricingTemplates">,
) {
  sharedPricingTemplates.set(revision, data);
}

export function getSharedLoadbalanceStrategies(revision: number, forceRefresh = false) {
  return sharedLoadbalanceStrategies.get(revision, forceRefresh);
}

export function setSharedLoadbalanceStrategies(
  revision: number,
  data: ReferenceDataValue<"loadbalanceStrategies">,
) {
  sharedLoadbalanceStrategies.set(revision, data);
}
