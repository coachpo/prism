import { useMemo } from "react";

import { useBanPolicyMutations } from "./useBanPolicyMutations";
import { useBanPolicyStrategyCollection } from "./useBanPolicyStrategyCollection";
import { useStrategyImpactPager } from "./useStrategyImpactPager";

/**
 * Compose the Ban Policy page's collection, mutation, and impact lifecycles.
 * The feature hook owns only their page-facing handoff.
 */
export function useBanPoliciesFeatureData(revision: number) {
  const collection = useBanPolicyStrategyCollection(revision);
  const {
    commitStrategies,
    defaultsCompleteness,
    loadStrategy,
    markReadError,
    refreshStrategies,
    refreshStrategiesAfterMutation,
    strategiesFragment,
  } = collection;
  const strategyIds = useMemo(
    () => (strategiesFragment.data ?? []).map((strategy) => strategy.id),
    [strategiesFragment.data],
  );
  const impact = useStrategyImpactPager(strategyIds);
  const mutations = useBanPolicyMutations({
    strategiesFragment,
    commitStrategies,
    loadStrategy,
    markReadError,
    refreshStrategies,
    refreshStrategiesAfterMutation,
  });

  return {
    defaultsCompleteness,
    refreshStrategies,
    strategiesFragment,
    ...impact,
    ...mutations,
  };
}
