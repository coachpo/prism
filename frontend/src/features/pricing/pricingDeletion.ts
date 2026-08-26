export function isPricingTemplateDeleteBlocked(options: {
  deleting: boolean;
  usageLoading: boolean;
  usageError: boolean;
  dependencyCount: number;
}) {
  return (
    options.deleting ||
    options.usageLoading ||
    options.usageError ||
    options.dependencyCount > 0
  );
}
