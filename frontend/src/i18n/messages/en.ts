export interface Messages {
  auth: {
    accountResetCodeSent: string;
    authenticating: string;
    backToLogin: string;
    browserNoPasskeys: string;
    enterResetCode: string;
    forgotPassword: string;
    forgotPasswordDescription: string;
    forgotPasswordError: string;
    forgotPasswordQuestion: string;
    keepSignedInFor: string;
    loginFailed: string;
    newPassword: string;
    orContinueWith: string;
    password: string;
    passwordUpdated: string;
    passkeyAuthenticationFailed: string;
    resetCode: string;
    resetPassword: string;
    resetPasswordError: string;
    resetPasswordTitle: string;
    resetPasswordDescription: string;
    resetting: string;
    sendCode: string;
    sending: string;
    session7Days: string;
    session30Days: string;
    sessionCurrent: string;
    signIn: string;
    signInDescription: string;
    signInToContinue: string;
    signInWithPasskey: string;
    signingIn: string;
    username: string;
    usernameOrEmail: string;
  };
  common: {
    apiFamily: string;
    close: string;
    connected: string;
    connecting: string;
    copiedToClipboard: (label: string) => string;
    copy: string;
    copyFailed: (label: string) => string;
    disconnected: string;
    endpointWithId: (id: string) => string;
    loadingApplication: string;
    notApplicable: string;
    profileFallback: string;
    reconnecting: string;
    requestFailed: string;
    syncing: string;
    unavailable: string;
    vendor: string;
    vendorIconLabel: (label: string) => string;
    vendorIconPlaceholder: string;
  };
  dashboard: {
    activeModels: string;
    averageRpm: string;
    avgLatency: string;
    errorRate: string;
    estimatedCost: string;
    inspectSpendingBreakdown: string;
    dashboardDescription: string;
    dashboardTitle: string;
    noApiFamilyActivity: string;
    noApiFamilyActivityDescription: string;
    noRecentActivity: string;
    noRecentActivityDescription: string;
    noSpendingData: string;
    noSpendingDataDescription: string;
    openStatistics: string;
    performanceSnapshot: string;
    performanceSnapshotDescription: string;
    routingDiagramLoadFailed: string;
    apiFamilyMix: string;
    apiFamilyMixDescription: string;
    quickActions: string;
    quickActionsDescription: string;
    routingStrategyMix: string;
    recentActivity: string;
    recentActivityDescription: string;
    refreshDashboard: string;
    requests24h: string;
    reviewRequests: string;
    routing24hErrors: string;
    routing24hHealth: string;
    routing24hSuccessRate: string;
    routing24hSuccessfulRequests: string;
    routing24hTotalRequests: string;
    routingActionOpenModelDetail: string;
    routingActiveConnections: string;
    routingChartActionHint: string;
    routingChartHint: string;
    routingEndpoint: string;
    routingEndpointNodeType: string;
    routingLegendDegraded: string;
    routingLegendFailing: string;
    routingLegendHealthy: string;
    routingLegendNoData: string;
    routingLegendNoRecentRequests: string;
    routingLink: string;
    routingLinkAria: (endpoint: string, model: string) => string;
    routingModel: string;
    routingModelNodeType: string;
    routingNoActiveRoutes: string;
    routingNoActiveRoutesDescription: string;
    routingNoData: string;
    routingNoDataDescription: string;
    routingNoRecentTraffic: string;
    routingNoRecentTrafficDescription: string;
    routingNodeType: string;
    routingTitle: string;
    routingDescription: string;
    routingLoadingDescription: string;
    spending30d: string;
    streamingShare: string;
    successfulRequests24h: (count: string) => string;
    activeRoutes: (count: string) => string;
    endpointCount: (count: string) => string;
    modelCount: (count: string) => string;
    strategyFamilyCount: (label: string, count: string) => string;
    totalConfigured: (count: string) => string;
    totalRequests: (count: string) => string;
    successRate: (rate: string) => string;
    p95Latency: string;
    topSpendingModels: string;
    topSpendingModelsDescription: string;
    viewFullReport: string;
  };
  locale: {
    changeLanguage: string;
    label: string;
    options: Record<"en" | "zh-CN", string>;
  };
  nav: {
    apiKeys: string;
    dashboard: string;
    endpoints: string;
    loadbalanceStrategies: string;
    models: string;
    pricingTemplates: string;
    requestLogs: string;
    settings: string;
    statistics: string;
  };
  loadbalanceStrategyDialog: {
    addTitle: string;
    addStatusCode: string;
    autoRecoveryDisabledOption: string;
    autoRecoveryEnabledOption: string;
    autoRecoveryLabel: string;
    banDurationDescription: string;
    banDurationLabel: string;
    banModeDescription: string;
    banModeLabel: string;
    banModeManualOption: string;
    banModeOffOption: string;
    banModeTemporaryOption: string;
    backoffMultiplierDescription: string;
    backoffMultiplierLabel: string;
    baseCooldownDescription: string;
    baseCooldownLabel: string;
    cancel: string;
    description: string;
    editTitle: string;
    explainField: (label: string) => string;
    failureThresholdDescription: string;
    failureThresholdLabel: string;
    failureStatusCodesDescription: string;
    failureStatusCodesLabel: string;
    jitterRatioDescription: string;
    jitterRatioLabel: string;
    maxCooldownStrikesBeforeBanDescription: string;
    maxCooldownStrikesBeforeBanLabel: string;
    maxCooldownDescription: string;
    maxCooldownLabel: string;
    legacyStrategyTypeLabel: string;
    nameLabel: string;
    namePlaceholder: string;
    removeStatusCode: (code: number) => string;
    routingPolicyLabel: string;
    save: string;
    saving: string;
    strategyFamilyLabel: string;
    strategyTypeLabel: string;
  };
  loadbalanceStrategyCopy: {
    adaptiveFamilyLabel: string;
    fillFirstLabel: string;
    fillFirstSummary: string;
    legacyFamilyLabel: string;
    maximizeAvailabilityLabel: string;
    maximizeAvailabilitySummary: string;
    minimizeLatencyLabel: string;
    minimizeLatencySummary: string;
    roundRobinLabel: string;
    roundRobinSummary: string;
    singleLabel: string;
    singleSummary: string;
  };
  loadbalanceStrategiesPage: {
    description: string;
    selectedProfileFallback: string;
    scopeCallout: (profileLabel: string) => string;
  };
  loadbalanceEvents: {
    backoffMultiplier: string;
    banModeManual: string;
    banModeOff: string;
    banModeTemporary: string;
    banMode: string;
    bannedUntil: string;
    connection: string;
    connectionId: string;
    consecutiveFailures: string;
    context: string;
    cooldown: string;
    cooldownValue: (seconds: string) => string;
    created: string;
    detailsTitle: string;
    endpointId: string;
    event: string;
    eventId: (id: number | null) => string;
    eventType: string;
    eventTypeBanned: string;
    eventTypeExtended: string;
    eventTypeMaxCooldownStrike: string;
    eventTypeNotOpened: string;
    eventTypeOpened: string;
    eventTypeProbeEligible: string;
    eventTypeRecovered: string;
    failedToLoadEventDetails: string;
    failureKind: string;
    failureKindConnectError: string;
    failureKindTimeout: string;
    failureKindTransientHttp: string;
    failureThreshold: string;
    failoverConfiguration: string;
    loadingEvents: string;
    maxCooldownSeconds: string;
    maxCooldownStrikes: string;
    modelId: string;
    next: string;
    noEventsRecorded: string;
    operation: string;
    previous: string;
    refresh: string;
    profileId: string;
    reason: string;
    showingEvents: (start: string, end: string, total: string) => string;
    summary: string;
    tabDescription: string;
    tabTitle: string;
    tableConnection: string;
    tableCooldown: string;
    tableCreated: string;
    tableEvent: string;
    tableFailure: string;
    tableFailures: string;
    tableId: string;
    vendorId: string;
    emptyDescription: string;
    emptyTitle: string;
  };
  loadbalanceStrategiesTable: {
    adaptiveRoutingSummary: (label: string) => string;
    actions: string;
    addStrategy: string;
    attachedModels: string;
    autoRecoveryDisabled: string;
    autoRecoveryEnabled: string;
    banManualDismiss: (strikes: string) => string;
    banOff: string;
    banTemporary: (strikes: string, durationSeconds: string) => string;
    cooldownSummary: (baseSeconds: string, maxSeconds: string) => string;
    description: string;
    disabled: string;
    edit: string;
    enabled: string;
    deleteStrategy: string;
    deleteStrategyDescription: (name: string) => string;
    deleteStrategyInUse: (count: string) => string;
    name: string;
    noStrategiesConfigured: string;
    recovery: string;
    statusCodes: (codes: string) => string;
    title: string;
    type: string;
  };
  loadbalanceStrategiesData: {
    created: string;
    deleted: string;
    deleteFailed: string;
    loadFailed: string;
    loadSingleFailed: string;
    saveFailed: string;
    updated: string;
  };
  loadbalanceStrategyValidation: {
    addStatusCode: string;
    backoffMultiplierRange: string;
    banDurationIntegerSeconds: string;
    banDurationManualDismissZero: string;
    banDurationTemporaryMin: string;
    banModeOffZero: string;
    baseCooldownIntegerSeconds: string;
    baseCooldownMin: string;
    failureThresholdInteger: string;
    failureThresholdRange: string;
    jitterRatioRange: string;
    maxCooldownIntegerSeconds: string;
    maxCooldownRange: string;
    maxCooldownStrikesInteger: string;
    maxCooldownStrikesMin: string;
    nameRequired: string;
    statusCodeExists: string;
    statusCodeIntegerRange: string;
    statusCodesUnique: string;
    statusCodesValidHttp: string;
  };
  pricingTemplateDialog: {
    addTitle: string;
    cacheCreationPriceLabel: string;
    cachedInputPriceLabel: string;
    cancel: string;
    currencyCodeLabel: string;
    currencyCodePlaceholder: string;
    description: string;
    descriptionLabel: string;
    descriptionPlaceholder: string;
    editTitle: string;
    inputPriceLabel: string;
    mapToOutputPrice: string;
    missingSpecialTokenPolicyHint: string;
    missingSpecialTokenPolicyLabel: string;
    nameLabel: string;
    namePlaceholder: string;
    outputPriceLabel: string;
    pricePlaceholder: string;
    reasoningPriceLabel: string;
    save: string;
    saving: string;
    zeroCost: string;
  };
  vendorManagement: {
    actions: string;
    addVendor: string;
    cancel: string;
    createVendor: string;
    delete: string;
    deleteDescription: (name: string) => string;
    deleteInUse: (count: string) => string;
    deleteTitle: string;
    dependencyApiFamily: string;
    dependencyModelId: string;
    dependencyModelType: string;
    dependencyProfile: string;
    descriptionLabel: string;
    descriptionPlaceholder: string;
    edit: string;
    editVendor: string;
    emptyDescription: string;
    emptyTitle: string;
    currentIconPreviewLabel: string;
    fallbackPreviewDescription: string;
    iconPresetFallbackOption: string;
    iconPresetHelp: string;
    iconPresetLabel: string;
    iconPresetPlaceholder: string;
    keyLabel: string;
    keyPlaceholder: string;
    nameLabel: string;
    namePlaceholder: string;
    noDescription: string;
    saveCreate: string;
    saveEdit: string;
    saving: string;
    sectionDescription: string;
    sectionTitle: string;
    tableDescription: string;
    tableKey: string;
    tableName: string;
    thisActionCannotBeUndone: string;
    vendorCreated: string;
    vendorDeleteFailed: string;
    vendorDeleted: string;
    vendorInUseDeleteBlocked: string;
    vendorKeyRequired: string;
    vendorNameRequired: string;
    vendorSaveFailed: string;
    vendorUpdated: string;
    vendorUsageLoadFailed: string;
  };
  settingsPage: {
    auditPrivacy: string;
    backup: string;
    billingCurrency: string;
    globalSettings: string;
    globalSettingsDescription: string;
    globalTab: string;
    profileScopedDescription: (profileLabel: string) => string;
    profileScopedSettings: string;
    profileTab: string;
    retentionDeletion: string;
    sectionsTitle: string;
    settingsDescription: string;
    settingsTitle: string;
    timezone: string;
  };
  settingsDialogs: {
    activateRuleImmediately: string;
    allData: string;
    blockHeadersExamples: string;
    blockHeadersTooltip: string;
    cancel: string;
    cleanupTypeAudits: string;
    cleanupTypeLoadbalanceEvents: string;
    cleanupTypeRequests: string;
    cleanupTypeStatistics: string;
    dataType: string;
    delete: string;
    deleteConfirmKeyword: string;
    deleteConfirmDescription: (profileLabel: string) => string;
    deleteConfirmTitle: string;
    deleteRuleDescription: (name: string) => string;
    deleteRuleTitle: string;
    deletionSummary: string;
    deleting: string;
    enabled: string;
    exactMatch: string;
    name: string;
    namePlaceholder: string;
    olderThanDays: (days: number | null) => string;
    pattern: string;
    patternPlaceholderExact: string;
    patternPlaceholderPrefix: string;
    prefixMatch: string;
    prefixMatchMustEndHyphen: string;
    ruleDialogAddDescription: string;
    ruleDialogAddTitle: string;
    ruleDialogEditDescription: string;
    ruleDialogEditTitle: string;
    retention: string;
    saveRule: string;
    stripSensitiveHeaders: string;
    type: string;
    typeDeleteToProceed: (keyword: string) => string;
    whyBlockHeaders: string;
  };
  settingsAuditRules: {
    addRule: string;
    customRules: string;
    description: string;
    loadingRules: string;
    noCustomRules: string;
    noSystemRules: string;
    systemRulesLocked: string;
  };
  settingsRetentionDeletion: {
    allData: string;
    dangerDescription: (profileLabel: string) => string;
    dataType: string;
    deletionFailed: string;
    deletionRequested: (label: string) => string;
    deleteData: string;
    deleteOlderThan: string;
    description: (profileLabel: string) => string;
    invalidRetentionOption: string;
    retentionDays: (days: number) => string;
    selectDataType: string;
    selectRetention: string;
    title: string;
  };
  settingsSaveState: {
    saved: string;
    unsavedChanges: string;
  };
  settingsFx: {
    decimalPlacesLimit: (max: number) => string;
    duplicateMapping: (modelId: string, endpointId: number) => string;
    rateForMapping: (modelId: string, endpointId: number, message: string) => string;
    rateMustBeGreaterThanZero: string;
    rateRequired: string;
  };
  settingsAuth: {
    passwordMaxLength: (max: number) => string;
    passwordMinLength: (min: number) => string;
  };
  settingsAuthentication: {
    addPasskey: string;
    authentication: string;
    authenticationDisabled: string;
    authenticationDisabledDescription: string;
    authenticationIsDisabled: string;
    authenticationStatus: string;
    authenticationToggleDescription: string;
    backupCapable: string;
    backupReady: string;
    continue: string;
    created: (date: string) => string;
    deviceName: string;
    deviceNamePlaceholder: string;
    deviceBound: string;
    emailAddress: string;
    emailRequired: string;
    emailVerificationFailed: string;
    emailVerificationSucceeded: string;
    enableAuthenticationToEnforceKeys: string;
    enableAuthenticationToManagePasskeys: string;
    lastUsed: (value: string) => string;
    noPasskeysRegistered: string;
    noPasskeysRegisteredDescription: string;
    notUsedYet: string;
    operatorAccount: string;
    operatorAccountDescription: string;
    password: string;
    confirmPassword: string;
    passwordConfirmationHelp: string;
    passwordKeepCurrent: string;
    passwordsMustMatch: string;
    passkeys: string;
    passkeysRegistered: (count: string) => string;
    proxyKeyTrafficRequirement: string;
    recoveryEmail: string;
    recoveryEmailDescription: string;
    recoveryEmailChangedRequiresVerification: string;
    recoveryEmailPlaceholder: string;
    resendCode: string;
    saveAccountChanges: string;
    sendVerificationCode: string;
    sendingCode: string;
    synced: string;
    syncedToAccount: string;
    unknownDate: string;
    unknownLastUse: string;
    verificationCode: string;
    verificationCodeRequired: string;
    verificationCodeSent: string;
    verificationCodeSentTo: (email: string) => string;
    verificationCodePrompt: string;
    verify: string;
    verifyEmail: string;
    verified: string;
    verifiedEmail: string;
    verifying: string;
    verificationOtpPlaceholder: string;
    registerPasskey: string;
    registerPasskeyDescription: string;
    registering: string;
    passkeyFallbackName: (id: number | string) => string;
    removeItem: (name: string) => string;
    removePasskey: string;
    removePasskeyConfirmation: (name: string) => string;
    removing: string;
    unsupportedPasskeys: string;
    username: string;
    usernameHelper: string;
    usernamePlaceholder: string;
  };
  settingsPasskeysData: {
    deviceNameRequired: string;
    loadFailed: string;
    registerFailed: string;
    registered: string;
    removeFailed: string;
    removed: string;
  };
  settingsAudit: {
    audit: string;
    auditAndPrivacy: string;
    bodies: string;
    bodiesSensitive: string;
    captureAndPrivacyDefaults: string;
    headerBlocklist: string;
    noVendorsAvailable: string;
    off: string;
    on: string;
    outputsMayBeCaptured: string;
    recordMetadata: string;
    stripsHeadersBeforeSendingUpstream: string;
  };
  settingsAuditData: {
    deleteRuleFailed: string;
    loadHeaderRulesFailed: string;
    loadVendorsFailed: string;
    nameAndPatternRequired: string;
    prefixPatternsHyphen: string;
    ruleCreated: string;
    ruleDeleted: string;
    ruleUpdated: string;
    saveRuleFailed: string;
    updateRuleFailed: string;
    updateVendorFailed: string;
  };
  settingsBackup: {
    acknowledgement: string;
    export: string;
    exportConfiguration: string;
    exportDescription: string;
    exportInProgress: string;
    exportRestoreSnapshots: (profileLabel: string) => string;
    exportsContainApiKeys: string;
    import: string;
    importConfiguration: string;
    importDescription: string;
    importInProgress: string;
    loadedSummary: (fileName: string, endpoints: string, strategies: string, models: string, connections: string) => string;
    previewBlockingErrors: string;
    previewReady: string;
    previewWarnings: string;
    title: string;
  };
  settingsBackupData: {
    acknowledgeSecretsBeforeExport: string;
    exportFailed: string;
    exportSucceeded: string;
    importFailed: string;
    importSucceeded: (endpoints: string, strategies: string, models: string, connections: string) => string;
    invalidConfigPayload: (errors: string) => string;
    invalidJsonFile: string;
  };
  settingsBackupValidation: {
    duplicateFxMapping: (modelId: string, endpointName: string) => string;
    duplicateProxyTarget: (targetModelId: string, modelId: string) => string;
    duplicateReferenceName: (referenceLabel: string, normalizedName: string) => string;
    fxMappingMustReferenceImportedPair: (modelId: string, endpointName: string) => string;
    missingEndpointName: string;
    missingReferenceName: string;
    modelMustIncludeVendorKey: (modelId: string) => string;
    nativeModelMustIncludeStrategy: (modelId: string) => string;
    nativeModelMustNotIncludeProxyTargets: (modelId: string) => string;
    proxyModelMustNotIncludeStrategy: (modelId: string) => string;
    proxyTargetsContiguous: (modelId: string) => string;
    referenceLabelEndpoint: string;
    referenceLabelLoadbalanceStrategy: string;
    referenceLabelPricingTemplate: string;
    referenceLabelVendor: string;
    referenceNameEmpty: (referenceLabel: string) => string;
    statusCodesUnique: string;
    unknownEndpointName: (endpointName: string) => string;
    unknownLoadbalanceStrategy: (strategyName: string) => string;
    unknownPricingTemplateName: (templateName: string) => string;
    unknownVendorKey: (vendorKey: string) => string;
  };
  costingUi: {
    default1To1: string;
    endpointSpecificRate: string;
    mapToOutputPrice: string;
    missingEndpoint: string;
    missingPriceData: string;
    missingTokenUsage: string;
    per1mTokens: string;
    pricingDisabled: string;
    zeroCost: string;
  };
  settingsBilling: {
    addMapping: string;
    billingAndCurrency: string;
    cancelFxMappingEdit: string;
    code: string;
    costApiUnavailable: string;
    currencyCodePlaceholder: string;
    currencySymbolPlaceholder: string;
    deleteFxMapping: string;
    defaultFx: string;
    endpoint: string;
    endpointFxMappingsEmpty: string;
    exampleTimestamp: (timestamp: string, zone: string) => string;
    fxMappings: string;
    fxOverridesDefault: string;
    fxRate: string;
    editFxMapping: string;
    fxRatePlaceholder: string;
    loadingEndpoints: string;
    mappingSourceOverride: string;
    model: string;
    reportingCurrency: string;
    reportingCurrencySummary: (code: string, symbol: string) => string;
    saveFxMapping: string;
    saveTimezone: string;
    selectEndpoint: string;
    selectModel: string;
    selectTimezone: string;
    settingsApiUnavailable: string;
    symbol: string;
    timezone: string;
    timezoneAffectsTimestamps: string;
    timezonePreference: string;
    timezoneAuto: (zone: string) => string;
    usedForSpendingReports: string;
  };
  settingsCostingData: {
    billingSaved: string;
    endpointSelectionInvalid: string;
    fixMappingErrorsBeforeTimezone: string;
    loadConnectionsFailed: string;
    loadCostingFailed: string;
    loadModelsForFxFailed: string;
    mappingDuplicate: string;
    mappingFieldsRequired: string;
    reportCurrencyRequired: string;
    reportCurrencySymbolLength: string;
    saveBillingBeforeTimezone: string;
    saveFailed: string;
    timezoneSaved: string;
  };
  settingsTimezone: {
    unavailable: string;
  };
  profiles: {
    activate: string;
    activating: string;
    activateDescription: string;
    activateTitle: (name: string) => string;
    active: string;
    activeShort: (name: string) => string;
    cancel: string;
    clearSearch: string;
    create: string;
    createDescription: string;
    createNewProfile: string;
    createTitle: string;
    creating: string;
    currentActive: string;
    default: string;
    delete: string;
    deleteConfirmPhrase: (name: string) => string;
    deleteDescription: (name: string) => string;
    deleteSelected: string;
    deleteTitle: string;
    deleting: string;
    descriptionOptional: string;
    editDescription: string;
    editSelected: string;
    editTitle: string;
    learnMore: string;
    limitReached: string;
    loadingProfiles: string;
    locked: string;
    manageProfiles: string;
    initializeFailed: string;
    name: string;
    nameRequired: string;
    newActive: string;
    noDescription: string;
    noMatches: string;
    noProfilesDescription: string;
    noProfilesTitle: string;
    optionalPlaceholder: string;
    defaultProfileDeleteDisabled: string;
    activeProfileDeleteDisabled: string;
    selectProfileToDelete: string;
    selectProfileToEdit: string;
    lockedProfileEditDisabled: string;
    profileNamePlaceholder: string;
    profileTriggerTitle: (selected: string, active: string) => string;
    save: string;
    saving: string;
    searchPlaceholder: string;
    selectProfile: string;
    createFailed: string;
    createdProfile: (name: string) => string;
    updateFailed: string;
    updatedProfile: string;
    activateConflict: string;
    activateFailed: string;
    activatedProfile: (name: string) => string;
    deleteFailed: string;
    deletedProfile: (name: string) => string;
    tryDifferentSearchTerm: string;
    typeToConfirm: (value: string) => string;
  };
  endpointsPage: {
    addEndpoint: string;
    description: string;
    editEndpoint: string;
    filterAll: string;
    filterInUse: string;
    filterUnused: string;
    noEndpointsConfigured: string;
    noEndpointsConfiguredDescription: string;
    noEndpointsMatchFilters: string;
    noEndpointsMatchFiltersDescription: string;
    reorderDisabledWhileFilters: string;
    saveChanges: string;
    searchEndpoints: string;
    title: string;
  };
  endpointsUi: {
    apiKeyRequired: string;
    baseUrl: string;
    baseUrlInvalid: string;
    baseUrlPlaceholder: string;
    configureDetails: string;
    created: (date: string) => string;
    deleteEndpoint: string;
    deleteEndpointDescription: (name: string) => string;
    dragToReorder: (name: string) => string;
    duplicateEndpoint: (name: string) => string;
    editEndpoint: (name: string) => string;
    keepStoredKey: string;
    models: string;
    name: string;
    nameRequired: string;
    namePlaceholder: string;
    none: string;
  };
  endpointsData: {
    created: string;
    createFailed: string;
    deleted: string;
    deleteFailed: string;
    duplicatedAs: (name: string) => string;
    duplicateFailed: string;
    loadFailed: string;
    reorderedFailed: string;
    updated: string;
    updateFailed: string;
  };
  modelDetail: {
    active: string;
    addConnection: string;
    addConnectionToStartRouting: string;
    addHeader: string;
    avgCostPerRequest: string;
    backToModels: string;
    banned: string;
    cancel: string;
    checkedAt: (time: string) => string;
    checkingNow: string;
    connectionActions: string;
    connectionFallback: (id: number) => string;
    currentTargetLabel: (targetId: string) => string;
    connectionDialogDescription: string;
    connectionDisplayNamePlaceholder: string;
    connectionHealthy: string;
    connectionNameOptional: string;
    connectionUnhealthy: string;
    configuration: string;
    connections: string;
    connectionsLoadOnDemandDescription: string;
    consecutiveFailures: (count: number) => string;
    cooldownMinutes: (minutes: number) => string;
    cooldownMinutesSeconds: (minutes: number, seconds: number) => string;
    cooldownSeconds: (seconds: number) => string;
    copyModelIdAria: (modelId: string) => string;
    costOverview: string;
    createNew: string;
    created: string;
    currentStateBlocked: (
      failureSummary: string,
      cooldown: string,
      failureKind: string,
      blockedUntil: string | null,
    ) => string;
    currentStateCounting: (failureSummary: string, failureKind: string) => string;
    currentStateManualBan: string;
    currentStateProbeEligible: (
      cooldown: string,
      blockedUntil: string | null,
      failureKind: string,
    ) => string;
    currentStateTemporaryBan: (until: string | null) => string;
    customHeaders: string;
    delete: string;
    disabled: string;
    displayName: string;
    displayNamePlaceholder: string;
    dragToReorderConnection: (name: string) => string;
    edit: string;
    editable: string;
    editConnection: string;
    editModel: string;
    enabled: string;
    endpointApiKey: string;
    endpointApiKeyPlaceholder: string;
    endpointBaseUrl: string;
    endpointBaseUrlPlaceholder: string;
    endpointName: string;
    endpointNamePlaceholder: string;
    endpointSource: string;
    endpointSourceCreateHint: string;
    endpointSourceEditHint: string;
    failoverEvents: (count: string) => string;
    failoverLast: (value: string) => string;
    failoverSignals: string;
    failureCount: (count: number) => string;
    failureKindConnectError: string;
    failureKindTimeout: string;
    failureKindTransientHttp: string;
    failureKindUnknown: string;
    firstTarget: (targetId: string) => string;
    filterConnections: string;
    healthCheck: string;
    healthChecking: string;
    healthHealthy: string;
    healthUnknown: string;
    healthUnhealthy: string;
    headerKey: string;
    headerValue: string;
    includeInLoadBalancing: string;
    inactive: string;
    keyLabel: string;
    leaveBlankForUnlimited: string;
    loadbalanceStrategy: string;
    loadbalanceStrategyLabel: string;
    maxInFlightNonStream: string;
    maxInFlightStream: string;
    modelConfigurationAndConnectionRouting: string;
    modelIdLabel: string;
    modelSettingsDescription: string;
    modelSettingsTitle: string;
    noConnectionsConfigured: string;
    noConnectionsMatchFilter: string;
    noCustomHeadersConfigured: string;
    noCostDataAvailable: string;
    noLoadbalanceStrategiesAvailable: string;
    noProfileEndpointsFound: string;
    notCheckedYet: string;
    orderedPriorityRouting: string;
    pricingOff: string;
    pricingOn: string;
    pricingTemplate: string;
    pricingTemplateHint: string;
    pricingTemplatePlaceholder: string;
    proxyRouting: string;
    proxyTargets: string;
    proxyTargetsHint: string;
    qpsLimit: string;
    recoveryBlocked: string;
    recoveryCounting: string;
    resetRecoveryState: string;
    requests24h: string;
    requestsLabel: string;
    routingPriorityHint: string;
    sampled5xxRate: string;
    saveConnection: string;
    saveChanges: string;
    selectEndpoint: string;
    selectApiFamily: string;
    selectedEndpoint: (name: string) => string;
    selectEndpointPlaceholder: string;
    selectExisting: string;
    selectStrategy: string;
    selectVendor: string;
    spend24h: (currencyCode: string) => string;
    successfulRequests: (count: string) => string;
    routingObjective: string;
    strategyRecovery: string;
    testConnection: string;
    testingConnection: string;
    targets: (count: string) => string;
    totalCost: (currencyCode: string) => string;
    totalTokens: (count: string) => string;
    tryDifferentSearchTerm: string;
    typeNative: string;
    typeProxy: string;
    unknownEndpoint: string;
    unassigned: string;
    unpricedNoCostTracking: string;
    useEndpointNameFallback: (name: string | null) => string;
    viewRequestLogs: string;
  };
  modelDetailTabs: {
    connections: string;
    loadbalanceEvents: string;
  };
  modelsPage: {
    countDescription: (count: string) => string;
    newModel: string;
    searchModels: string;
    title: string;
  };
  modelsUi: {
    addTarget: string;
    allNativeModelsIncluded: string;
    deleteModel: string;
    deleteModelDescription: (name: string) => string;
    displayNameOptional: string;
    editModel: string;
    modelId: string;
    modelIdPlaceholder: string;
    noNativeModelsForFamily: (apiFamily: string) => string;
    noProxyTargetsSelected: string;
    optionalFriendlyName: string;
    priority: (value: string) => string;
    proxyTargetsDescriptionPrimary: string;
    proxyTargetsDescriptionSecondary: string;
    remainingNativeTargets: (count: string) => string;
    routingTypeDescription: string;
    save: string;
    strategyNotConfigured: string;
    targetMoveDown: (id: string) => string;
    targetMoveUp: (id: string) => string;
    targetRemove: (id: string) => string;
    viewModelDetails: (name: string) => string;
    noModelsMatchSearch: string;
    noModelsConfigured: string;
    tryDifferentModelNameOrId: string;
    createFirstModel: string;
    activeConnections: (active: string, total: string) => string;
    successLabel: string;
    requestsShort: string;
    spendShort: string;
    unknownVendor: string;
    noProxyTargets: string;
    targetsFirst: (count: string, first: string) => string;
    modelCount: (count: string) => string;
  };
  modelsData: {
    created: string;
    deleted: string;
    deleteFailed: string;
    fetchFailed: string;
    saveFailed: string;
    selectApiFamily: string;
    selectLoadbalanceStrategy: string;
    selectVendor: string;
    updated: string;
  };
  pricingTemplatesUi: {
    actions: string;
    addTemplate: string;
    close: string;
    currency: string;
    deletePricingTemplate: string;
    deletePricingTemplateDescription: (name: string) => string;
    deletePricingTemplateInUse: (count: string) => string;
    description: string;
    endpoint: string;
    input: string;
    model: string;
    noTemplatesConfigured: string;
    output: string;
    profileScopedSettings: string;
    scopeCallout: (profileLabel: string) => string;
    tableTitle: string;
    templateUsage: string;
    templateUsageDescription: (name: string) => string;
    templateUnused: string;
    title: string;
    unnamed: string;
    viewUsage: string;
  };
  pricingTemplatesData: {
    cacheCreationNonNegative: string;
    cachedInputNonNegative: string;
    changedWhileEditing: string;
    created: string;
    deleted: string;
    deleteFailed: string;
    endpointWithId: (id: string) => string;
    inUseCannotDelete: string;
    inputNonNegative: string;
    invalidCurrency: string;
    loadFailed: string;
    loadSingleFailed: string;
    loadUsageFailed: string;
    nameRequired: string;
    unknownModel: string;
    outputNonNegative: string;
    reasoningNonNegative: string;
    saveFailed: string;
    updated: string;
  };
  proxyApiKeys: {
    actions: string;
    active: string;
    apiKey: string;
    authenticationOff: string;
    authenticationOn: string;
    authenticationUnavailable: string;
    copyKey: string;
    createDescription: string;
    createKey: string;
    createProxyKey: string;
    creating: string;
    created: string;
    deleteKey: string;
    deleteProxyApiKey: string;
    deleteProxyApiKeyDescription: (name: string, prefix: string) => string;
    deleteProxyKeyAria: (name: string) => string;
    description: string;
    disabled: string;
    editDescription: string;
    editProxyApiKey: string;
    editProxyKeyAria: (name: string) => string;
    issuedKeys: string;
    keyCount: (count: string) => string;
    keyLimitReached: string;
    keysPreparedDescription: string;
    keysProtectedDescription: string;
    keysUsed: (used: string, limit: string) => string;
    lastIp: string;
    lastUsed: string;
    listDescription: string;
    name: string;
    nameNote: string;
    namePlaceholder: string;
    newSecret: string;
    newSecretDescription: string;
    noInternalNote: string;
    noProxyKeysCreated: string;
    notes: string;
    notesPlaceholder: string;
    operation: string;
    preview: string;
    rotateProxyKeyAria: (name: string) => string;
    slotsRemaining: (remaining: string) => string;
    title: string;
    never: string;
    unknown: string;
    updated: string;
  };
  proxyApiKeysData: {
    created: string;
    createFailed: string;
    deleted: string;
    deleteFailed: string;
    keyNameRequired: string;
    loadAuthStatusFailed: string;
    loadKeysFailed: string;
    maxKeysReached: (limit: string) => string;
    rotated: string;
    rotateFailed: string;
    settingsUnavailable: string;
    updated: string;
    updateFailed: string;
  };
  modelDetailData: {
    connectionFallback: (id: string) => string;
    connectionCreated: string;
    connectionDeleted: string;
    connectionTestFailed: string;
    connectionUpdated: string;
    fetchModelDetailsFailed: string;
    deleteConnectionFailed: string;
    fillEndpointFields: string;
    healthCheckResult: (status: string, latencyMs: string) => string;
    healthCheckFailed: string;
    loadRecoveryStateFailed: string;
    modelUpdated: string;
    proxyTargetsUpdated: string;
    reorderPriorityReverted: string;
    resetRecoveryStateFailed: string;
    saveConnectionFailed: string;
    selectApiFamily: string;
    selectEndpoint: string;
    selectLoadbalanceStrategy: string;
    selectVendor: string;
    toggleConnectionFailed: string;
    updateModelFailed: string;
    updateProxyTargetsFailed: string;
  };
  requestLogs: {
    allColumns: string;
    allConnections: string;
    allEndpoints: string;
    allModels: string;
    allStatuses: string;
    any: string;
    anyLatency: string;
    anyOutcome: string;
    audit: string;
    billableOnly: string;
    cacheCreation: string;
    cacheRead: string;
    compact: string;
    connection: string;
    detailDescription: string;
    endpoint: string;
    fourHundredsOnly: string;
    last6Hours: string;
    last24Hours: string;
    last30Days: string;
    last7Days: string;
    lastHour: string;
    latency: string;
    latencyFast: string;
    latencyNormal: string;
    latencySlow: string;
    latencyVerySlow: string;
    localRefinement: string;
    loadFailed: string;
    max: string;
    min: string;
    model: string;
    nonStreaming: string;
    outcome: string;
    overview: string;
    pricedOnly: string;
    reasoning: string;
    refreshRequestLogs: string;
    requestId: string;
    requestTitle: (id: number | string) => string;
    requestNotFound: string;
    requestNotFoundDescription: (id: string) => string;
    requestLogsAllTime: string;
    requestLogsDescription: string;
    requestLogsTitle: string;
    noCaptured: (title: string) => string;
    noRequestLogsMatchSlice: string;
    requestBody: string;
    requestHeaders: string;
    search: string;
    searchPlaceholder: string;
    relaxScope: string;
    returnToRequestList: string;
    resultsRange: (start: string, end: string, total: string) => string;
    response: (status: number) => string;
    rowsPerPage: string;
    specialTokens: string;
    status: string;
    stream: string;
    streaming: string;
    technicalInspection: string;
    tokens: string;
    requestDetails: string;
    requestedModel: string;
    proxyOrigin: string;
    resolvedTarget: string;
    time: string;
    totalCost: string;
    totalTokens: string;
    timestamp: string;
    errorDetail: string;
    ingressRequestId: string;
    attemptNumber: string;
    providerCorrelationId: string;
    formattedForReadability: string;
    capturedFailureDetail: string;
    copy: string;
    path: string;
    routingContext: string;
    tokenUsage: string;
    costBreakdown: string;
    input: string;
    output: string;
    total: string;
    priced: string;
    billable: string;
    yes: string;
    no: string;
    whyUnpriced: string;
    baseUrl: string;
    auditCapture: string;
    auditCaptureUnavailable: string;
    auditCaptureDisabledForVendor: string;
    auditLoadFailedTitle: string;
    auditLoadFailed: string;
    noAuditRecords: string;
    timeRange: string;
    tokenRange: string;
    triage: string;
    view: string;
    fiveHundredsOnly: string;
    spend: string;
    viewRequestInLogs: string;
    viewingRequest: (id: string) => string;
    exit: string;
    zeroResults: string;
  };
  requestLogsDetail: {
    connectionNotFound: string;
    copyFailed: (label: string) => string;
    copied: (label: string) => string;
  };
  shell: {
     activate: string;
     activateProfile: string;
     activating: string;
     activeRuntime: (name: string) => string;
     aligned: string;
    collapseSidebar: string;
    closeSidebar: string;
    expandSidebar: string;
    logoutFailed: string;
    mismatch: string;
     mismatchWarning: (selected: string, active: string) => string;
     openSidebar: string;
     out: string;
    primaryNavigation: string;
    profile: string;
    profileRuntime: string;
    runtime: string;
    signOut: string;
    viewing: string;
  };
  statistics: {
    addLine: string;
    averageRpm: string;
    adjustFiltersOrTimeRange: string;
    aggregation: string;
    all: string;
    allConnections: string;
    allModels: string;
    allRows: string;
    anyError: string;
    availability: string;
    byDay: string;
    byHour: string;
    billableOnlyRequests?: string;
    cacheHitRate: string;
    cachedRows: (count: string) => string;
    clearFilters: string;
    connection: string;
    costOverviewTitle: string;
    costByBucket: string;
    costComponentsBy: (groupBy: string) => string;
    costEfficiencyScatter: string;
    costInsights: string;
    currentRpm: string;
    debug: string;
    errors: string;
    fourxxRate: string;
    fivexxRate: string;
    group: string;
    groupBy: string;
    filters: string;
    filtersApplyToAllSpending: string;
    from: string;
    health: string;
    highestOneMinuteThroughput: string;
    highestSpend: string;
    input: string;
    inputOutputSpecial: string;
    noSpendingDataFound: string;
    loadingThroughputData: string;
    latencyDistribution: string;
    latencyPercentiles: string;
    mostRecentOneMinuteBucket: string;
    mostFrequentErrorSignatures: string;
    noCostRecordsFound: string;
    operationsDescription: string;
    operationsTab: string;
    noDataPointsAvailable: string;
    noErrorSignaturesFound: string;
    noHttpErrorsInSlice: string;
    noRequestsFound: string;
    noThroughputDataAvailable: string;
    output: string;
    peakRpm: string;
    p95Latency: string;
    p99Latency: string;
    percentTotal: string;
    pricedPercent: string;
    vendorLabel: string;
    refreshThroughputStatistics: string;
    refreshOperationsStatistics: string;
    refreshSpendingStatistics: string;
    refreshUsageStatistics: string;
    reset: string;
    customRange: string;
    lastHour: string;
    last6Hours: string;
    last24Hours: string;
    last7Days: string;
    last30Days: string;
    allTime: string;
    today: string;
    day: string;
    week: string;
    month: string;
    endpointGroup: string;
    endpointStatisticsTitle: string;
    exportSnapshotJson: string;
    modelGroup: string;
    lineLimitReached: string;
    linesSelected: (count: string, max: string) => string;
    linesToDisplay: string;
    modelEndpointGroup: string;
    modelStatisticsTitle: string;
    noEndpointStatisticsDescription: string;
    noEndpointStatisticsTitle: string;
    noModelStatisticsDescription: string;
    noModelStatisticsTitle: string;
    requestsInWindow: (count: string) => string;
    noProxyApiKeyUsageDescription: string;
    noProxyApiKeyUsageTitle: string;
    openPricingTemplates: string;
    overviewTitle: string;
    pricingDataMissingDescription: string;
    pricingDataMissingTitle: string;
    proxyApiKey: string;
    proxyApiKeyStatisticsTitle: string;
    removeLine: (label: string) => string;
    previousPage: string;
    nextPage: string;
    requestTrendsTitle: string;
    requestsTab: string;
    requests: string;
    requestsPerMinuteOverTime: string;
    rows: string;
    selectModelLinePlaceholder: string;
    serviceHealthTitle: string;
    slow: string;
    slowestRequests: string;
    rowsPerPage?: string;
    spend: string;
    spendingDescription: string;
    spendingTab: string;
    spendingBreakdown: string;
    tokenTypeBreakdownTitle: string;
    tokenUsageTrendsTitle: string;
    specialTokenCoverageVisibleRows: string;
    cachedCaptured: string;
    cachedPrefix: string;
    connectionId: string;
    costly: string;
    currency: string;
    dollarsPerMillionTokens: string;
    dollarsPerRequest: string;
    modelId: string;
    noDataAvailable: string;
    reasoningCaptured: string;
    anySpecialCaptured: string;
    failedCount: (count: string) => string;
    failedToLoadEndpointModelStatistics: string;
    failedToLoadUsageStatistics: string;
    healthStatusDegraded: string;
    healthStatusDown: string;
    healthStatusIdle: string;
    healthStatusOk: string;
    heatmapLegendLessAvailability: string;
    heatmapLegendMoreAvailability: string;
    latest: string;
    loadingEndpointModelStatistics: string;
    noTokenUsage: string;
    oldest: string;
    serviceHealthIntervalHours: (count: number) => string;
    serviceHealthIntervalMinutes: (count: number) => string;
    successful: (count: string) => string;
    successfulCount: (count: string) => string;
    serviceHealthWindowDays: (count: number) => string;
    successOnly: string;
    successRate: string;
    specialTokens: string;
    statisticsDescription: string;
    statisticsTitle: string;
    topHttpErrors: string;
    timeWindow: string;
    timeWindowTotal: (seconds: string) => string;
    to: string;
    totalSpend: string;
    totalTokens: string;
    throughputExplanation: string;
    throughputTab: string;
    tokens: string;
    tokenThroughput: string;
    topN: string;
    topEndpointsByCost: string;
    topModelsByCost: string;
    totalRequests: (count: string) => string;
    updated: string;
    unpriced: (count: string) => string;
    unpricedBreakdown: string;
    unknownProxyApiKey: string;
    usageAndCost: string;
    usageStatisticsPagePlaceholder: string;
    performance: string;
    requestOutcomeOverTime: string;
  };
  theme: {
    changeTheme: string;
    dark: string;
    light: string;
    system: string;
  };
}

export const enMessages: Messages = {
  auth: {
    accountResetCodeSent: "If the account matches, a reset code has been sent.",
    authenticating: "Authenticating...",
    backToLogin: "Back to login",
    browserNoPasskeys:
      "Your browser does not support Passkeys. Please use a modern browser or try another login method.",
    enterResetCode: "Enter reset code",
    forgotPassword: "Forgot password?",
    forgotPasswordDescription: "Enter the bound username or email to receive a reset code.",
    forgotPasswordError: "Failed to request password reset",
    forgotPasswordQuestion: "Forgot password?",
    keepSignedInFor: "Keep me signed in for",
    loginFailed: "Login failed",
    newPassword: "New password",
    orContinueWith: "Or continue with",
    password: "Password",
    passwordUpdated: "Password updated. Sign in with your new password.",
    passkeyAuthenticationFailed: "Passkey authentication failed",
    resetCode: "Reset code",
    resetPassword: "Reset password",
    resetPasswordDescription: "Use the emailed OTP and choose a new password.",
    resetPasswordError: "Failed to reset password",
    resetPasswordTitle: "Reset password",
    resetting: "Resetting...",
    sendCode: "Send code",
    sending: "Sending...",
    session7Days: "7 days",
    session30Days: "30 days",
    sessionCurrent: "Current browser session",
    signIn: "Sign in",
    signInDescription: "Sign in to manage Prism settings, profiles, and routing.",
    signInToContinue: "Authentication enabled. Sign in to continue.",
    signInWithPasskey: "Sign in with Passkey",
    signingIn: "Signing in...",
    username: "Username",
    usernameOrEmail: "Username or email",
  },
  common: {
    apiFamily: "API Family",
    close: "Close",
    connected: "Connected",
    connecting: "Connecting...",
    copiedToClipboard: (label) => `${label} copied to clipboard`,
    copy: "Copy",
    copyFailed: (label) => `Failed to copy ${label.toLowerCase()}`,
    disconnected: "Disconnected",
    endpointWithId: (id) => `Endpoint ${id}`,
    loadingApplication: "Loading application...",
    notApplicable: "N/A",
    profileFallback: "profile",
    reconnecting: "Reconnecting...",
    requestFailed: "Request failed",
    syncing: "Syncing...",
    unavailable: "Unavailable",
    vendor: "Vendor",
    vendorIconLabel: (label) => `Vendor icon ${label}`,
    vendorIconPlaceholder: "Vendor icon placeholder",
  },
  dashboard: {
    activeModels: "Active Models",
    averageRpm: "Average RPM",
    avgLatency: "Avg Latency",
    dashboardDescription: "System overview and health status",
    dashboardTitle: "Dashboard",
    errorRate: "Error Rate",
    estimatedCost: "Estimated cost",
    inspectSpendingBreakdown: "Inspect Spending Breakdown",
    noRecentActivity: "No recent activity",
    noRecentActivityDescription: "Requests will appear here once processed.",
    noSpendingData: "No spending data",
    noSpendingDataDescription: "Cost data will appear here once requests are priced.",
  noApiFamilyActivity: "No API family activity",
    noApiFamilyActivityDescription: "API family request distribution appears after traffic is processed.",
    openStatistics: "Open Statistics",
    performanceSnapshot: "Performance Snapshot",
    performanceSnapshotDescription: "Current operational profile (24h)",
    routingDiagramLoadFailed:
      "Routing diagram data could not be loaded. The rest of the dashboard is still available.",
  apiFamilyMix: "API Family Mix",
  apiFamilyMixDescription: "Request distribution by API family (24h)",
    quickActions: "Quick Actions",
    quickActionsDescription: "Jump to focused spending analysis",
    routingStrategyMix: "Routing strategy mix",
    recentActivity: "Recent Activity",
    recentActivityDescription: "Latest requests processed by the gateway",
    refreshDashboard: "Refresh dashboard",
    requests24h: "24h Requests",
    reviewRequests: "Review Requests",
    routing24hErrors: "24h errors",
    routing24hHealth: "24h health",
    routing24hSuccessRate: "24h success rate",
    routing24hSuccessfulRequests: "24h successful requests",
    routing24hTotalRequests: "24h total requests",
    routingActionOpenModelDetail: "Open model detail",
    routingActiveConnections: "Active connections",
    routingChartActionHint: "Click model nodes to open details",
    routingChartHint: "Link width reflects active connection count. Color reflects 24h route success rate.",
    routingEndpoint: "Endpoint",
    routingEndpointNodeType: "Endpoint",
    routingLegendDegraded: "Degraded",
    routingLegendFailing: "Failing",
    routingLegendHealthy: "Healthy",
    routingLegendNoData: "No data",
    routingLegendNoRecentRequests: "No recent requests",
    routingLink: "Routing link",
    routingLinkAria: (endpoint, model) => `Route from ${endpoint} to ${model}`,
    routingModel: "Model",
    routingModelNodeType: "Model",
    routingNoActiveRoutes: "No active routes",
    routingNoActiveRoutesDescription:
      "Activate at least one model connection to map live routing paths across endpoints and models.",
    routingNoData: "No routing data",
    routingNoDataDescription: "No routing diagram data is available for this profile.",
    routingNoRecentTraffic: "No routed traffic in the last 24h",
    routingNoRecentTrafficDescription:
      "Active routes are configured, but no successful request traffic was recorded for the current profile in the last 24 hours.",
    routingNodeType: "Node type",
    routingTitle: "Routing Health Map",
    routingDescription:
      "Trace active endpoint-to-model paths in one view. Link width reflects active route count, while color reflects 24-hour route health.",
    routingLoadingDescription: "Loading live routing volume and 24-hour health data",
    spending30d: "30d Spending",
    streamingShare: "Streaming Share",
    successfulRequests24h: (count) => `${count} successful requests in 24h`,
    activeRoutes: (count) => `${count} active route${count === "1" ? "" : "s"}`,
    endpointCount: (count) => `${count} endpoint${count === "1" ? "" : "s"}`,
    modelCount: (count) => `${count} model${count === "1" ? "" : "s"}`,
    strategyFamilyCount: (label, count) => `${label} ${count}`,
    totalConfigured: (count) => `of ${count} total configured`,
    totalRequests: (count) => `${count} total requests`,
    successRate: (rate) => `${rate}% success rate`,
    p95Latency: "P95 Latency",
    topSpendingModels: "Top Spending Models",
    topSpendingModelsDescription: "Highest cost models (last 30 days)",
    viewFullReport: "View Full Report",
  },
  locale: {
    changeLanguage: "Change language",
    label: "Language",
    options: {
      en: "English",
      "zh-CN": "简体中文",
    },
  },
  nav: {
    apiKeys: "API Keys",
    dashboard: "Dashboard",
    endpoints: "Endpoints",
    loadbalanceStrategies: "Loadbalance Strategies",
    models: "Models",
    pricingTemplates: "Pricing Templates",
    requestLogs: "Request Logs",
    settings: "Settings",
    statistics: "Statistics",
  },
  loadbalanceStrategyDialog: {
    addTitle: "Add Loadbalance Strategy",
    addStatusCode: "Add Status Code",
    autoRecoveryDisabledOption: "Disabled",
    autoRecoveryEnabledOption: "Enabled",
    autoRecoveryLabel: "Auto Recovery",
    banDurationDescription:
      "How long a temporary ban lasts before the connection becomes probe-eligible again.",
    banDurationLabel: "Ban Duration (seconds)",
    banModeDescription:
      "Choose whether repeated max-open strikes stay off, expire automatically, or wait for a manual dismiss.",
    banModeLabel: "Ban Mode",
    banModeManualOption: "Manual dismiss",
    banModeOffOption: "Off",
    banModeTemporaryOption: "Temporary",
    backoffMultiplierDescription:
      "Multiplier applied to the open window after each failure beyond the threshold.",
    backoffMultiplierLabel: "Backoff Multiplier",
    baseCooldownDescription:
      "Starting open window applied after transient failures once the threshold is reached.",
    baseCooldownLabel: "Base Open Window (seconds)",
    cancel: "Cancel",
    description: "Configure reusable legacy load-balance strategies for native models in this profile.",
    editTitle: "Edit Loadbalance Strategy",
    explainField: (label) => `Explain ${label}`,
    failureThresholdDescription:
      "Number of consecutive failures required before the circuit breaker opens.",
    failureThresholdLabel: "Failure Threshold",
    failureStatusCodesDescription:
      "HTTP status codes that should count toward automatic recovery.",
    failureStatusCodesLabel: "Failure Status Codes",
    jitterRatioDescription:
      "Random spread applied to the open window so retries do not all happen at the same instant.",
    jitterRatioLabel: "Jitter Ratio",
    maxCooldownStrikesBeforeBanDescription:
      "Number of max-open strike events required before this connection is marked as banned.",
    maxCooldownStrikesBeforeBanLabel: "Max Open Strikes Before Ban",
    maxCooldownDescription:
      "Upper limit for the computed open window, even after repeated failures.",
    maxCooldownLabel: "Max Open Window (seconds)",
    legacyStrategyTypeLabel: "Legacy Strategy Type",
    nameLabel: "Name",
    namePlaceholder: "e.g. round-robin-primary",
    removeStatusCode: (code) => `Remove status code ${code}`,
    routingPolicyLabel: "Routing Policy",
    save: "Save Strategy",
    saving: "Saving...",
    strategyFamilyLabel: "Strategy Family",
    strategyTypeLabel: "Strategy Type",
  },
  loadbalanceStrategyCopy: {
    adaptiveFamilyLabel: "Adaptive strategy",
    fillFirstLabel: "Fill first",
    fillFirstSummary: "Keep using the first eligible connection until it becomes unavailable.",
    legacyFamilyLabel: "Legacy strategy",
    maximizeAvailabilityLabel: "Maximize availability",
    maximizeAvailabilitySummary: "Adaptive routing that prioritizes the healthiest available path.",
    minimizeLatencyLabel: "Minimize latency",
    minimizeLatencySummary: "Adaptive routing that prefers the fastest healthy path.",
    roundRobinLabel: "Round robin",
    roundRobinSummary: "Rotate the starting connection across eligible connections.",
    singleLabel: "Single",
    singleSummary: "Use the first eligible connection and fall through on failure.",
  },
  loadbalanceStrategiesPage: {
    description:
      "Manage reusable legacy and adaptive native-model strategies for this profile",
    selectedProfileFallback: "the selected profile",
    scopeCallout: (profileLabel) =>
      `Changes here affect ${profileLabel} and native models attached to these strategies.`,
  },
  loadbalanceEvents: {
    backoffMultiplier: "Backoff Multiplier",
    banModeManual: "Manual dismiss",
    banModeOff: "Off",
    banModeTemporary: "Temporary",
    banMode: "Ban Mode",
    bannedUntil: "Banned Until",
    connection: "Connection",
    connectionId: "Connection ID",
    consecutiveFailures: "Consecutive Failures",
    context: "Context",
    cooldown: "Cooldown",
    cooldownValue: (seconds) => `${seconds}s`,
    created: "Created",
    detailsTitle: "Loadbalance Event Details",
    endpointId: "Endpoint ID",
    event: "Event",
    eventId: (id) => `Event ID: ${id ?? "-"}`,
    eventType: "Event Type",
    eventTypeBanned: "Banned",
    eventTypeExtended: "Extended",
    eventTypeMaxCooldownStrike: "Max Cooldown Strike",
    eventTypeNotOpened: "Not Opened",
    eventTypeOpened: "Opened",
    eventTypeProbeEligible: "Probe Eligible",
    eventTypeRecovered: "Recovered",
    failedToLoadEventDetails: "Failed to load event details",
    failureKind: "Failure Kind",
    failureKindConnectError: "Connection Error",
    failureKindTimeout: "Timeout",
    failureKindTransientHttp: "Transient HTTP",
    failureThreshold: "Failure Threshold",
    failoverConfiguration: "Failover Configuration",
    loadingEvents: "Loading loadbalance events...",
    maxCooldownSeconds: "Max Cooldown (seconds)",
    maxCooldownStrikes: "Max Cooldown Strikes",
    modelId: "Model ID",
    next: "Next",
    noEventsRecorded: "No loadbalance events recorded for this model yet.",
    operation: "Operation",
    previous: "Previous",
    refresh: "Refresh loadbalance events",
    profileId: "Profile ID",
    reason: "Reason",
    showingEvents: (start, end, total) => `Showing ${start} to ${end} of ${total} events`,
    summary: "Summary",
    tabDescription: "Recent failover, recovery, and ban activity for this model.",
    tabTitle: "Loadbalance Events",
    tableConnection: "Connection",
    tableCooldown: "Cooldown",
    tableCreated: "Created",
    tableEvent: "Event",
    tableFailure: "Failure",
    tableFailures: "Failures",
    tableId: "ID",
    vendorId: "Vendor ID",
    emptyDescription: "This model has not recorded any failover or recovery activity.",
    emptyTitle: "No loadbalance events yet",
  },
  loadbalanceStrategiesTable: {
    adaptiveRoutingSummary: (label) => `Routing policy ${label}`,
    actions: "Actions",
    addStrategy: "Add Strategy",
    attachedModels: "Attached Models",
    autoRecoveryDisabled: "Auto recovery disabled",
    autoRecoveryEnabled: "Auto recovery enabled",
    banManualDismiss: (strikes) => `Ban manual dismiss after ${strikes} max-cooldown strikes`,
    banOff: "Ban off",
    banTemporary: (strikes, durationSeconds) =>
      `Temporary ban after ${strikes} max-cooldown strikes • ${durationSeconds}s`,
    cooldownSummary: (baseSeconds, maxSeconds) =>
      `Cooldown ${baseSeconds}s base • ${maxSeconds}s max`,
    description:
      "Reuse legacy and adaptive load-balance strategies across native models instead of redefining routing behavior per model.",
    disabled: "Disabled",
    edit: "Edit",
    enabled: "Enabled",
    deleteStrategy: "Delete Loadbalance Strategy",
    deleteStrategyDescription: (name) => `Are you sure you want to delete the strategy "${name}"?`,
    deleteStrategyInUse: (count) => `This strategy is attached to ${count} native model${count === "1" ? "" : "s"} and cannot be deleted yet.`,
    name: "Name",
    noStrategiesConfigured: "No loadbalance strategies configured.",
    recovery: "Recovery",
    statusCodes: (codes) => `Status codes ${codes}`,
    title: "Loadbalance Strategies",
    type: "Type",
  },
  loadbalanceStrategiesData: {
    created: "Loadbalance strategy created",
    deleted: "Loadbalance strategy deleted",
    deleteFailed: "Failed to delete loadbalance strategy",
    loadFailed: "Failed to load loadbalance strategies",
    loadSingleFailed: "Failed to load loadbalance strategy",
    saveFailed: "Failed to save loadbalance strategy",
    updated: "Loadbalance strategy updated",
  },
  loadbalanceStrategyValidation: {
    addStatusCode: "Add at least one failure status code",
    backoffMultiplierRange: "Backoff multiplier must be between 1 and 10",
    banDurationIntegerSeconds: "Ban duration must be a whole number of seconds",
    banDurationManualDismissZero: "Ban duration must be 0 seconds for manual dismiss bans",
    banDurationTemporaryMin: "Ban duration must be at least 1 second for temporary bans",
    banModeOffZero: "Ban escalation must stay at 0 strikes and 0 seconds while ban mode is off",
    baseCooldownIntegerSeconds: "Base open window must be a whole number of seconds",
    baseCooldownMin: "Base open window must be at least 0 seconds",
    failureThresholdInteger: "Failure threshold must be a whole number",
    failureThresholdRange: "Failure threshold must be between 1 and 50",
    jitterRatioRange: "Jitter ratio must be between 0 and 1",
    maxCooldownIntegerSeconds: "Max open window must be a whole number of seconds",
    maxCooldownRange: "Max open window must be between 1 and 86400 seconds",
    maxCooldownStrikesInteger: "Max open strikes before ban must be a whole number",
    maxCooldownStrikesMin: "Max open strikes before ban must be at least 1 when ban escalation is enabled",
    nameRequired: "Name is required",
    statusCodeExists: "That status code is already included",
    statusCodeIntegerRange: "Status code must be a whole number between 100 and 599",
    statusCodesUnique: "Failure status codes must be unique",
    statusCodesValidHttp: "Failure status codes must be valid HTTP status codes between 100 and 599",
  },
  pricingTemplateDialog: {
    addTitle: "Add Pricing Template",
    cacheCreationPriceLabel: "Cache Creation Price (Optional)",
    cachedInputPriceLabel: "Cached Input Price (Optional)",
    cancel: "Cancel",
    currencyCodeLabel: "Currency Code",
    currencyCodePlaceholder: "USD",
    description: "Configure pricing rates per 1M tokens.",
    descriptionLabel: "Description (Optional)",
    descriptionPlaceholder: "Optional details about this template",
    editTitle: "Edit Pricing Template",
    inputPriceLabel: "Input Price (per 1M tokens)",
    mapToOutputPrice: "Map to Output Price",
    missingSpecialTokenPolicyHint:
      "How to price special tokens (like reasoning) if their specific price is not set.",
    missingSpecialTokenPolicyLabel: "Missing Special Token Policy",
    nameLabel: "Name",
    namePlaceholder: "e.g., GPT-4o Standard",
    outputPriceLabel: "Output Price (per 1M tokens)",
    pricePlaceholder: "0.00",
    reasoningPriceLabel: "Reasoning Price (Optional)",
    save: "Save Template",
    saving: "Saving...",
    zeroCost: "Zero Cost",
  },
  vendorManagement: {
    actions: "Actions",
    addVendor: "Add Vendor",
    cancel: "Cancel",
    createVendor: "Add Vendor",
    delete: "Delete",
    deleteDescription: (name) => `Are you sure you want to delete the vendor "${name}"?`,
    deleteInUse: (count) => `This vendor is referenced by ${count} model${count === "1" ? "" : "s"} and cannot be deleted yet.`,
    deleteTitle: "Delete Vendor",
    dependencyApiFamily: "API Family",
    dependencyModelId: "Model ID",
    dependencyModelType: "Model Type",
    dependencyProfile: "Profile",
    descriptionLabel: "Description (Optional)",
    descriptionPlaceholder: "Optional details about this vendor",
    edit: "Edit",
    editVendor: "Edit Vendor",
    emptyDescription: "Create a shared vendor entry here to make it available across profiles.",
    emptyTitle: "No vendors configured",
    currentIconPreviewLabel: "Current icon preview",
    fallbackPreviewDescription: "If no preset fits, Prism falls back to a letter monogram.",
    iconPresetFallbackOption: "No preset (use fallback)",
    iconPresetHelp: "Choose a bundled vendor mark when one fits this vendor.",
    iconPresetLabel: "Icon preset",
    iconPresetPlaceholder: "Select an icon preset",
    keyLabel: "Vendor Key",
    keyPlaceholder: "e.g. openai",
    nameLabel: "Vendor Name",
    namePlaceholder: "e.g. OpenAI",
    noDescription: "No description",
    saveCreate: "Create Vendor",
    saveEdit: "Save Vendor",
    saving: "Saving...",
    sectionDescription: "Manage the shared vendor catalog used by models and audit defaults across all profiles.",
    sectionTitle: "Vendor Management",
    tableDescription: "Description",
    tableKey: "Key",
    tableName: "Name",
    thisActionCannotBeUndone: "This action cannot be undone.",
    vendorCreated: "Vendor created",
    vendorDeleteFailed: "Failed to delete vendor",
    vendorDeleted: "Vendor deleted",
    vendorInUseDeleteBlocked: "Cannot delete this vendor because it is still in use",
    vendorKeyRequired: "Vendor key is required",
    vendorNameRequired: "Vendor name is required",
    vendorSaveFailed: "Failed to save vendor",
    vendorUpdated: "Vendor updated",
    vendorUsageLoadFailed: "Failed to load vendor usage",
  },
  settingsPage: {
    auditPrivacy: "Audit & Privacy",
    backup: "Backup",
    billingCurrency: "Billing & Currency",
    globalSettings: "Global settings",
    globalSettingsDescription: "Changes here apply to all profiles and the entire Prism instance.",
    globalTab: "Global",
    profileScopedDescription: (profileLabel) => `Changes here affect ${profileLabel} and its runtime traffic.`,
    profileScopedSettings: "Profile-scoped settings",
    profileTab: "Profile",
    retentionDeletion: "Retention & Deletion",
    sectionsTitle: "Settings Sections",
    settingsDescription: "Manage instance-wide authentication and profile-scoped configuration",
    settingsTitle: "Settings",
    timezone: "Timezone",
  },
  settingsDialogs: {
    activateRuleImmediately: "Activate this rule immediately",
    allData: "All data",
    blockHeadersExamples:
      'Examples: cf- (prefix), x-forwarded-for (exact).',
    blockHeadersTooltip:
      "Blocklist rules prevent privacy, tunnel, and tracing metadata from reaching upstream providers.",
    cancel: "Cancel",
    cleanupTypeAudits: "Audit Logs",
    cleanupTypeLoadbalanceEvents: "Loadbalance Events",
    cleanupTypeRequests: "Request Logs",
    cleanupTypeStatistics: "Statistics Data",
    dataType: "Data type",
    delete: "Delete",
    deleteConfirmKeyword: "DELETE",
    deleteConfirmDescription: (profileLabel) => `This deletes data in ${profileLabel} and cannot be undone.`,
    deleteConfirmTitle: "Confirm Deletion",
    deleteRuleDescription: (name) =>
      `Are you sure you want to delete the rule "${name}"? This action cannot be undone.`,
    deleteRuleTitle: "Delete Rule",
    deletionSummary: "Deletion summary",
    deleting: "Deleting...",
    enabled: "Enabled",
    exactMatch: "Exact Match",
    name: "Name",
    namePlaceholder: "e.g. Remove Tunnel Headers",
    olderThanDays: (days) => `Older than ${days ?? "-"} days`,
    pattern: "Pattern",
    patternPlaceholderExact: "x-request-id",
    patternPlaceholderPrefix: "cf-",
    prefixMatch: "Prefix Match",
    prefixMatchMustEndHyphen: "Prefix patterns must end with a hyphen (-).",
    ruleDialogAddDescription:
      "Create a custom rule to block headers before requests are sent upstream.",
    ruleDialogAddTitle: "Add Rule",
    ruleDialogEditDescription: "Modify an existing custom header blocklist rule.",
    ruleDialogEditTitle: "Edit Rule",
    retention: "Retention",
    saveRule: "Save Rule",
    stripSensitiveHeaders: "Use this to strip sensitive headers before forwarding runtime traffic.",
    type: "Type",
    typeDeleteToProceed: (keyword) => `Type ${keyword} to proceed`,
    whyBlockHeaders: "Why block headers",
  },
  settingsAuditRules: {
    addRule: "Add Rule",
    customRules: "Custom rules",
    description:
      "Use header rules to block privacy, tunnel, and tracing metadata before forwarding requests upstream.",
    loadingRules: "Loading rules...",
    noCustomRules: "No custom rules. Add one to strip private headers before forwarding.",
    noSystemRules: "No system rules found.",
    systemRulesLocked: "System rules (locked)",
  },
  settingsRetentionDeletion: {
    allData: "All data",
    dangerDescription: (profileLabel) => `This deletes data in ${profileLabel} and cannot be undone.`,
    dataType: "Data type",
    deleteData: "Delete data",
    deleteOlderThan: "Delete data older than",
    deletionFailed: "Deletion failed",
    deletionRequested: (label) => `${label} deletion requested`,
    description: (profileLabel) =>
      `Delete stored data in ${profileLabel} with explicit retention and confirmation controls.`,
    invalidRetentionOption: "Select a valid retention option",
    retentionDays: (days) => `${days} days`,
    selectDataType: "Select data type",
    selectRetention: "Select retention",
    title: "Retention & Deletion",
  },
  settingsSaveState: {
    saved: "Saved",
    unsavedChanges: "Unsaved changes",
  },
  settingsFx: {
    decimalPlacesLimit: (max) => `Use up to ${max} decimal places`,
    duplicateMapping: (modelId, endpointId) => `Duplicate FX mapping detected for ${modelId} #${endpointId}`,
    rateForMapping: (modelId, endpointId, message) => `FX rate for ${modelId} #${endpointId}: ${message}`,
    rateMustBeGreaterThanZero: "FX rate must be greater than zero",
    rateRequired: "FX rate is required",
  },
  settingsAuth: {
    passwordMaxLength: (max) => `Password must be at most ${max} characters`,
    passwordMinLength: (min) => `Password must be at least ${min} characters`,
  },
  settingsAuthentication: {
    addPasskey: "Add passkey",
    authentication: "Authentication",
    authenticationDisabled: "Authentication disabled",
    authenticationDisabledDescription: "Configure operator sign-in, recovery email, and passkeys for this Prism instance.",
    authenticationIsDisabled: "Authentication is disabled",
    authenticationStatus: "Authentication status",
    authenticationToggleDescription: "Sign-in can only be enabled after the operator account and recovery email are fully configured.",
    backupCapable: "Backup capable",
    backupReady: "Backup ready",
    continue: "Continue",
    created: (date) => `Created ${date}`,
    deviceName: "Device Name",
    deviceNamePlaceholder: "e.g., My MacBook Pro",
    deviceBound: "Device-bound",
    emailAddress: "Email address",
    emailRequired: "Email is required",
    emailVerificationFailed: "Failed to verify email",
    emailVerificationSucceeded: "Email verified",
    enableAuthenticationToEnforceKeys: "Enable authentication in Settings when you are ready to enforce these keys.",
    enableAuthenticationToManagePasskeys: "Enable authentication to register and manage passkeys.",
    lastUsed: (value) => `Last used ${value}`,
    noPasskeysRegistered: "No passkeys registered",
    noPasskeysRegisteredDescription:
      "Add a passkey to sign in with biometrics or your device lock screen instead of typing a password every time.",
    notUsedYet: "Not used yet",
    operatorAccount: "Operator account",
    operatorAccountDescription: "Configure the single local operator identity used to sign in.",
    password: "Password",
    confirmPassword: "Confirm password",
    passwordConfirmationHelp: "Repeat the password exactly to confirm it.",
    passwordKeepCurrent: "Leave blank to keep the current password.",
    passwordsMustMatch: "Passwords must match before you can continue.",
    passkeys: "Passkeys",
    passkeysRegistered: (count) => `${count} registered`,
    passkeyFallbackName: (id) => `Passkey #${id}`,
    proxyKeyTrafficRequirement: "Requests to `/v1/*` and `/v1beta/*` must present a valid key.",
    recoveryEmail: "Recovery email",
    recoveryEmailDescription: "Verify a recovery email before authentication can be turned on.",
    recoveryEmailChangedRequiresVerification:
      "If you change the recovery email, you must verify the new address with OTP.",
    recoveryEmailPlaceholder: "operator@example.com",
    resendCode: "Resend code",
    saveAccountChanges: "Save account changes",
    sendVerificationCode: "Send verification code",
    sendingCode: "Sending code...",
    synced: "Synced",
    syncedToAccount: "Synced to your account",
    unknownDate: "Unknown date",
    unknownLastUse: "Unknown last use",
    verificationCode: "Verification code",
    verificationCodeRequired: "Verification code is required",
    verificationCodeSent: "Verification code sent",
    verificationCodeSentTo: (email) => `A verification code was sent to ${email}. Enter it below to confirm.`,
    verificationCodePrompt: "Send a verification code after changing the email address.",
    verify: "Verify",
    verifyEmail: "Verify email",
    verified: "Verified",
    verifiedEmail: "Verified email",
    verifying: "Verifying...",
    verificationOtpPlaceholder: "OTP",
    registerPasskey: "Register Passkey",
    registerPasskeyDescription: "Give this device a name to help you identify it later.",
    registering: "Registering...",
    removeItem: (name) => `Remove ${name}`,
    removePasskey: "Remove Passkey",
    removePasskeyConfirmation: (name) =>
      `Are you sure you want to remove the passkey "${name}"? You will no longer be able to use this device to sign in.`,
    removing: "Removing...",
    unsupportedPasskeys: "Your browser or device does not support Passkeys (WebAuthn).",
    username: "Username",
    usernameHelper: "This will be the only local sign-in name for this Prism instance.",
    usernamePlaceholder: "admin",
  },
  settingsPasskeysData: {
    deviceNameRequired: "Device name is required",
    loadFailed: "Failed to load passkeys",
    registerFailed: "Failed to register passkey",
    registered: "Passkey registered successfully",
    removeFailed: "Failed to remove passkey",
    removed: "Passkey removed successfully",
  },
  settingsAudit: {
    audit: "Audit",
    auditAndPrivacy: "Audit & Privacy",
    bodies: "Bodies",
    bodiesSensitive: "Include request/response bodies (sensitive).",
    captureAndPrivacyDefaults: "Configure vendor-level audit capture and privacy defaults.",
    headerBlocklist: "Header Blocklist",
    noVendorsAvailable: "No vendors available.",
    off: "Off",
    on: "On",
    outputsMayBeCaptured: "May capture prompts/outputs.",
    recordMetadata: "Record request/response metadata.",
    stripsHeadersBeforeSendingUpstream: "Strips headers before sending upstream.",
  },
  settingsAuditData: {
    deleteRuleFailed: "Failed to delete rule",
    loadHeaderRulesFailed: "Failed to load header blocklist rules",
    loadVendorsFailed: "Failed to load vendors",
    nameAndPatternRequired: "Name and pattern are required",
    prefixPatternsHyphen: "Prefix patterns must end with a hyphen (-)",
    ruleCreated: "Rule created successfully",
    ruleDeleted: "Rule deleted successfully",
    ruleUpdated: "Rule updated successfully",
    saveRuleFailed: "Failed to save rule",
    updateRuleFailed: "Failed to update rule",
    updateVendorFailed: "Failed to update vendor",
  },
  settingsBackup: {
    acknowledgement: "I understand this export includes endpoint API keys.",
    export: "Export",
    exportConfiguration: "Export Configuration",
    exportDescription: "Download a profile bundle with encrypted endpoint secrets and profile-scoped configuration.",
    exportInProgress: "Exporting...",
    exportRestoreSnapshots: (profileLabel) => `Export or restore configuration snapshots for ${profileLabel}.`,
    exportsContainApiKeys: "Exports include encrypted endpoint secrets and can be imported only on instances that use the matching bundle key.",
    import: "Import",
    importConfiguration: "Import Configuration",
    importDescription: "Upload a version 2 profile bundle, review the preview result, and then restore this profile's configuration.",
    importInProgress: "Importing...",
    loadedSummary: (fileName, endpoints, strategies, models, connections) =>
      `Loaded ${fileName}: ${endpoints} endpoints, ${strategies} strategies, ${models} models, ${connections} connections.`,
    previewBlockingErrors: "Preview blocking errors",
    previewReady: "Preview ready for import",
    previewWarnings: "Preview warnings",
    title: "Backup",
  },
  settingsBackupData: {
    acknowledgeSecretsBeforeExport: "Acknowledge that endpoint API keys are included before exporting.",
    exportFailed: "Export failed",
    exportSucceeded: "Configuration exported successfully",
    importFailed: "Import failed",
    importSucceeded: (endpoints, strategies, models, connections) => `Imported ${endpoints} endpoints, ${strategies} strategies, ${models} models, ${connections} connections`,
    invalidConfigPayload: (errors) => `Invalid configuration payload: ${errors}`,
    invalidJsonFile: "Invalid JSON file",
  },
  settingsBackupValidation: {
    duplicateFxMapping: (modelId, endpointName) =>
      `Duplicate FX mapping for model_id='${modelId}', endpoint_name='${endpointName}'`,
    duplicateProxyTarget: (targetModelId, modelId) =>
      `Duplicate proxy target '${targetModelId}' for model '${modelId}'`,
    duplicateReferenceName: (referenceLabel, normalizedName) =>
      `Duplicate ${referenceLabel} name '${normalizedName}'`,
    fxMappingMustReferenceImportedPair: (modelId, endpointName) =>
      `FX mapping must reference an imported model/endpoint pair: model_id='${modelId}', endpoint_name='${endpointName}'`,
    missingEndpointName: "Must include endpoint_name",
    missingReferenceName: "Must include a reference name",
    modelMustIncludeVendorKey: (modelId) => `Model '${modelId}' must include vendor_key`,
    nativeModelMustIncludeStrategy: (modelId) =>
      `Native model '${modelId}' must include loadbalance_strategy_name`,
    nativeModelMustNotIncludeProxyTargets: (modelId) =>
      `Native model '${modelId}' must not include proxy_targets`,
    proxyModelMustNotIncludeStrategy: (modelId) =>
      `Proxy model '${modelId}' must not include loadbalance_strategy_name`,
    proxyTargetsContiguous: (modelId) =>
      `Proxy targets for '${modelId}' must use contiguous positions starting at 0`,
    referenceLabelEndpoint: "endpoint",
    referenceLabelLoadbalanceStrategy: "loadbalance strategy",
    referenceLabelPricingTemplate: "pricing template",
    referenceLabelVendor: "vendor",
    referenceNameEmpty: (referenceLabel) => `${referenceLabel} name must not be empty`,
    statusCodesUnique: "Failover status codes must be unique",
    unknownEndpointName: (endpointName) =>
      `Unknown endpoint_name '${endpointName}' in import payload`,
    unknownLoadbalanceStrategy: (strategyName) =>
      `Unknown loadbalance strategy '${strategyName}' in import payload`,
    unknownPricingTemplateName: (templateName) =>
      `Unknown pricing_template_name '${templateName}' in import payload`,
    unknownVendorKey: (vendorKey) => `Unknown vendor_key '${vendorKey}' in import payload`,
  },
  costingUi: {
    default1To1: "Default (1:1)",
    endpointSpecificRate: "Endpoint-specific rate",
    mapToOutputPrice: "Map to output price",
    missingEndpoint: "Missing endpoint",
    missingPriceData: "Missing price data",
    missingTokenUsage: "Missing token usage",
    per1mTokens: "Per 1M tokens",
    pricingDisabled: "Pricing disabled",
    zeroCost: "Zero cost",
  },
  settingsBilling: {
    addMapping: "Add Mapping",
    billingAndCurrency: "Billing & Currency",
    cancelFxMappingEdit: "Cancel FX mapping edit",
    code: "Code",
    costApiUnavailable: "Costing settings API is currently unavailable.",
    currencyCodePlaceholder: "USD",
    currencySymbolPlaceholder: "$",
    deleteFxMapping: "Delete FX mapping",
    defaultFx: "Default FX = 1.0",
    endpoint: "Endpoint",
    endpointFxMappingsEmpty: "No endpoint FX mappings configured.",
    exampleTimestamp: (timestamp, zone) => `Example timestamp: ${timestamp} (${zone})`,
    fxMappings: "FX mappings",
    fxOverridesDefault: "Mapping overrides default.",
    fxRate: "FX rate",
    editFxMapping: "Edit FX mapping",
    fxRatePlaceholder: "1.000000",
    loadingEndpoints: "Loading endpoints...",
    mappingSourceOverride: "Override",
    model: "Model",
    reportingCurrency: "Reporting currency",
    reportingCurrencySummary: (code, symbol) => `Reporting currency: ${code} (${symbol})`,
    saveFxMapping: "Save FX mapping",
    saveTimezone: "Save timezone",
    selectEndpoint: "Select endpoint",
    selectModel: "Select model",
    selectTimezone: "Select timezone",
    settingsApiUnavailable: "Settings API is currently unavailable.",
    symbol: "Symbol",
    timezone: "Timezone",
    timezoneAffectsTimestamps: "Timezone preference affects timestamp rendering across the dashboard.",
    timezonePreference: "Timezone preference",
    timezoneAuto: (zone) => `Auto (Browser: ${zone})`,
    usedForSpendingReports: "Used for spending reports and dashboards.",
  },
  settingsCostingData: {
    billingSaved: "Billing and currency settings saved",
    endpointSelectionInvalid: "Invalid endpoint selection",
    fixMappingErrorsBeforeTimezone: "Fix billing and currency mapping errors before saving timezone.",
    loadConnectionsFailed: "Failed to load connections for selected model",
    loadCostingFailed: "Failed to load costing settings",
    loadModelsForFxFailed: "Failed to load models for FX mapping",
    mappingDuplicate: "Duplicate FX mapping for selected model and endpoint",
    mappingFieldsRequired: "Model, endpoint, and FX rate are required",
    reportCurrencyRequired: "Reporting currency must be a valid 3-letter code (for example, USD)",
    reportCurrencySymbolLength: "Reporting currency symbol must be 5 characters or fewer",
    saveBillingBeforeTimezone: "Save billing and currency settings before saving timezone.",
    saveFailed: "Failed to save settings",
    timezoneSaved: "Timezone saved",
  },
  settingsTimezone: {
    unavailable: "Unavailable",
  },
  profiles: {
    activate: "Activate",
    activating: "Activating...",
    activateDescription:
      "This will switch the active runtime profile. Existing traffic will route using the newly active profile.",
    activateTitle: (name) => `Activate "${name}" for runtime traffic?`,
    active: "Active",
    activeShort: (name) => `Active: ${name}`,
    cancel: "Cancel",
    clearSearch: "Clear search",
    create: "Create",
    createDescription:
      "Create a new management scope profile. Runtime traffic is unaffected until activation.",
    createNewProfile: "Create new profile",
    createTitle: "Create Profile",
    creating: "Creating...",
    currentActive: "Current active:",
    default: "Default",
    delete: "Delete",
    deleteDescription: (name) => `Delete selected profile ${name}. This action is irreversible.`,
    deleteConfirmPhrase: (name) => `delete ${name}`,
    deleteSelected: "Delete selected",
    deleteTitle: "Delete Profile",
    deleting: "Deleting...",
    descriptionOptional: "Description (Optional)",
    editDescription: "Update selected profile metadata. This does not activate runtime traffic.",
    editSelected: "Edit selected",
    editTitle: "Edit Profile",
    learnMore: "Learn more",
    limitReached: "You've reached the limit (10). Delete an inactive profile to create a new one.",
    loadingProfiles: "Loading profiles...",
    locked: "Locked",
    manageProfiles: "Manage profiles",
    initializeFailed: "Failed to initialize profiles",
    name: "Name",
    nameRequired: "Profile name is required",
    newActive: "New active:",
    noDescription: "No description",
    noMatches: "No matches",
    noProfilesDescription: "Create a profile to start routing traffic or running tests.",
    noProfilesTitle: "No profiles yet",
    optionalPlaceholder: "Optional",
    defaultProfileDeleteDisabled: "Default profile cannot be deleted.",
    activeProfileDeleteDisabled: "Active runtime profile cannot be deleted.",
    selectProfileToDelete: "Select a profile to delete.",
    selectProfileToEdit: "Select a profile to edit.",
    lockedProfileEditDisabled: "Default profile is locked and cannot be edited.",
    profileNamePlaceholder: "Profile name",
    profileTriggerTitle: (selected, active) => `Selected profile: ${selected}. Active runtime: ${active}.`,
    save: "Save",
    saving: "Saving...",
    searchPlaceholder: "Search profiles...",
    selectProfile: "Select profile",
    createFailed: "Failed to create profile",
    createdProfile: (name) => `Created profile ${name}`,
    updateFailed: "Failed to update profile",
    updatedProfile: "Profile updated",
    activateConflict:
      "Activation conflict detected. Active profile changed elsewhere, profile state was refreshed.",
    activateFailed: "Failed to activate profile",
    activatedProfile: (name) => `Activated ${name} for runtime traffic`,
    deleteFailed: "Failed to delete profile",
    deletedProfile: (name) => `Deleted profile ${name}`,
    tryDifferentSearchTerm: "Try a different search term.",
    typeToConfirm: (value) => `Type ${value} to confirm`,
  },
  modelDetail: {
    active: "Active",
    addConnection: "Add Connection",
    addConnectionToStartRouting: "Add a connection to start routing requests",
    addHeader: "Add Header",
    avgCostPerRequest: "Avg Cost / Request",
    backToModels: "Back to models",
    banned: "Banned",
    cancel: "Cancel",
    checkedAt: (time) => `Checked ${time}`,
    checkingNow: "Checking now...",
    connectionActions: "Connection actions",
    connectionFallback: (id) => `Connection ${id}`,
    currentTargetLabel: (targetId) => `${targetId} (current target)`,
    connectionDialogDescription:
      "Configure endpoint source and optional pricing template for this connection. Routing priority is managed from the connection list by dragging cards.",
    connectionDisplayNamePlaceholder: "Connection display name",
    connectionHealthy: "Connection Healthy",
    connectionNameOptional: "Name (Optional)",
    connectionUnhealthy: "Connection Unhealthy",
    configuration: "Configuration",
    connections: "Connections",
    connectionsLoadOnDemandDescription:
      "Connection metrics and health checks load on demand to avoid large page-open bursts.",
    consecutiveFailures: (count) => `${count} consecutive failure${count === 1 ? "" : "s"}`,
    cooldownMinutes: (minutes) => `${minutes}m`,
    cooldownMinutesSeconds: (minutes, seconds) => `${minutes}m ${seconds}s`,
    cooldownSeconds: (seconds) => `${seconds}s`,
    copyModelIdAria: (modelId) => `Copy model ID ${modelId}`,
    costOverview: "Cost Overview",
    createNew: "Create New",
    created: "Created",
    currentStateBlocked: (failureSummary, cooldown, failureKind, blockedUntil) =>
      `${failureSummary} triggered a ${cooldown} cooldown after ${failureKind}. Routing stays paused until ${blockedUntil ?? "the cooldown expires"}.`,
    currentStateCounting: (failureSummary, failureKind) =>
      `Tracking ${failureSummary} after ${failureKind}. No cooldown is currently open, but failover recovery is still counting these signals.`,
    currentStateManualBan: "This connection is banned until the operator dismisses it.",
    currentStateProbeEligible: (cooldown, blockedUntil, failureKind) =>
      `The last ${cooldown} cooldown expired${blockedUntil ? ` at ${blockedUntil}` : ""}. This connection is now eligible for the next routed probe after ${failureKind}.`,
    currentStateTemporaryBan: (until) =>
      `This connection is banned until ${until ?? "the temporary ban expires"}.`,
    customHeaders: "Custom Headers",
    delete: "Delete",
    disabled: "Disabled",
    displayName: "Display Name",
    displayNamePlaceholder: "Friendly name",
    dragToReorderConnection: (name) => `Drag to reorder connection ${name}`,
    edit: "Edit",
    editable: "Editable",
    editConnection: "Edit Connection",
    editModel: "Edit model",
    enabled: "Enabled",
    endpointApiKey: "API Key",
    endpointApiKeyPlaceholder: "sk-...",
    endpointBaseUrl: "Base URL",
    endpointBaseUrlPlaceholder: "https://api.openai.com",
    endpointName: "Name",
    endpointNamePlaceholder: "e.g. OpenAI Primary",
    endpointSource: "Endpoint Source",
    endpointSourceCreateHint: "Choose an existing endpoint or create one inline for this connection.",
    endpointSourceEditHint: "Switch this connection to another endpoint or create a new one.",
    failoverEvents: (count) => `Events: ${count}`,
    failoverLast: (value) => `Last: ${value}`,
    failoverSignals: "Failover-like signals (derived from 5xx)",
    failureCount: (count) => `${count} failure${count === 1 ? "" : "s"}`,
    failureKindConnectError: "a connection error",
    failureKindTimeout: "a timeout",
    failureKindTransientHttp: "a transient HTTP failure",
    failureKindUnknown: "an unknown failure",
    firstTarget: (targetId) => `First ${targetId}`,
    filterConnections: "Filter connections...",
    healthCheck: "Health Check",
    healthChecking: "Checking",
    healthHealthy: "Healthy",
    healthUnknown: "Unknown",
    healthUnhealthy: "Unhealthy",
    headerKey: "Header Key",
    headerValue: "Value",
    includeInLoadBalancing: "Include in load balancing",
    inactive: "Inactive",
    keyLabel: "Key",
    leaveBlankForUnlimited: "Leave blank for unlimited.",
    loadbalanceStrategy: "Loadbalance Strategy",
    loadbalanceStrategyLabel: "Loadbalance Strategy",
    maxInFlightNonStream: "Max In-Flight (Non-Stream)",
    maxInFlightStream: "Max In-Flight (Stream)",
    modelConfigurationAndConnectionRouting: "Model configuration and connection routing",
    modelIdLabel: "Model ID",
    modelSettingsDescription:
      "Update model identity, vendor metadata, and API family compatibility for this profile.",
    modelSettingsTitle: "Model Settings",
    noConnectionsConfigured: "No connections configured",
    noConnectionsMatchFilter: "No connections match your filter",
    noCustomHeadersConfigured: "No custom headers configured.",
    noCostDataAvailable: "No cost data available",
    noLoadbalanceStrategiesAvailable:
      "No loadbalance strategies are available for this profile. Create one on the Loadbalance Strategies page first.",
    noProfileEndpointsFound: "No profile endpoints found.",
    notCheckedYet: "Not checked yet",
    orderedPriorityRouting: "Ordered priority routing",
    pricingOff: "Pricing Off",
    pricingOn: "Pricing On",
    pricingTemplate: "Pricing Template",
    pricingTemplateHint: "Assign a pricing template to track costs for this connection.",
    pricingTemplatePlaceholder: "Select a pricing template...",
    proxyRouting: "Proxy Routing",
    proxyTargets: "Proxy Targets",
    proxyTargetsHint:
      "Manage ordered proxy targets from the dedicated card on this page. Proxy targets must stay on the same API family even when the vendor metadata changes.",
    qpsLimit: "QPS Limit",
    recoveryBlocked: "Recovery Blocked",
    recoveryCounting: "Recovery Counting",
    resetRecoveryState: "Reset Recovery State",
    requests24h: "Requests (24h)",
    requestsLabel: "Requests",
    routingPriorityHint:
      "New connections are appended as fallbacks. Drag and drop cards in the Model Detail list to adjust routing priority.",
    sampled5xxRate: "5xx rate (sampled)",
    saveConnection: "Save Connection",
    saveChanges: "Save Changes",
    selectEndpoint: "Select Endpoint",
    selectApiFamily: "Select API family",
    selectedEndpoint: (name) => `Selected: ${name}`,
    selectEndpointPlaceholder: "Select an endpoint...",
    selectExisting: "Select Existing",
    selectStrategy: "Select strategy",
    selectVendor: "Select vendor",
    spend24h: (currencyCode) => `Spend (24h, ${currencyCode})`,
    successfulRequests: (count) => `${count} successful`,
    routingObjective: "Strategy Type",
    strategyRecovery: "Strategy Recovery",
    testConnection: "Test Connection",
    testingConnection: "Testing...",
    targets: (count) => `${count} targets`,
    totalCost: (currencyCode) => `Total Cost (${currencyCode})`,
    totalTokens: (count) => `${count} tokens`,
    tryDifferentSearchTerm: "Try a different search term",
    typeNative: "Native",
    typeProxy: "Proxy",
    unknownEndpoint: "Unknown endpoint",
    unassigned: "Unassigned",
    unpricedNoCostTracking: "Unpriced (No cost tracking)",
    useEndpointNameFallback: (name) =>
      name ? `Leave blank to use endpoint name: ${name}.` : "Leave blank to use endpoint name.",
    viewRequestLogs: "View Request Logs",
  },
  modelDetailData: {
    connectionFallback: (id) => `Connection ${id}`,
    connectionCreated: "Connection created",
    connectionDeleted: "Connection deleted",
    connectionTestFailed: "Connection test failed",
    connectionUpdated: "Connection updated",
    fetchModelDetailsFailed: "Failed to fetch model details",
    deleteConnectionFailed: "Failed to delete connection",
    fillEndpointFields: "Please fill in all endpoint fields",
    healthCheckResult: (status, latencyMs) => `Health: ${status} (${latencyMs}ms)`,
    healthCheckFailed: "Health check failed",
    loadRecoveryStateFailed: "Failed to load recovery state",
    modelUpdated: "Model updated",
    proxyTargetsUpdated: "Proxy targets updated",
    reorderPriorityReverted: "Order reverted.",
    resetRecoveryStateFailed: "Failed to reset recovery state",
    saveConnectionFailed: "Failed to save connection",
    selectApiFamily: "Please select an API family",
    selectEndpoint: "Please select an endpoint",
    selectLoadbalanceStrategy: "Please select a loadbalance strategy for this native model",
    selectVendor: "Please select a vendor",
    toggleConnectionFailed: "Failed to toggle connection",
    updateModelFailed: "Failed to update model",
    updateProxyTargetsFailed: "Failed to update proxy targets",
  },
  modelDetailTabs: {
    connections: "Connections",
    loadbalanceEvents: "Loadbalance Events",
  },
  endpointsPage: {
    addEndpoint: "Add Endpoint",
    description: "Manage profile-scoped API credentials and model routing targets.",
    editEndpoint: "Edit Endpoint",
    filterAll: "All",
    filterInUse: "In Use",
    filterUnused: "Unused",
    noEndpointsConfigured: "No endpoints configured",
    noEndpointsConfiguredDescription: "Add your first endpoint to start routing requests.",
    noEndpointsMatchFilters: "No endpoints match your filters",
    noEndpointsMatchFiltersDescription: "Try a different search or clear the review filters.",
    reorderDisabledWhileFilters: "Reordering is disabled while review filters are active.",
    saveChanges: "Save Changes",
    searchEndpoints: "Search endpoints...",
    title: "Endpoints",
  },
  endpointsUi: {
    apiKeyRequired: "API Key is required",
    baseUrl: "Base URL",
    baseUrlInvalid: "Must be a valid URL",
    baseUrlPlaceholder: "https://api.openai.com",
    configureDetails: "Configure the endpoint details.",
    created: (date) => `Created ${date}`,
    deleteEndpoint: "Delete Endpoint",
    deleteEndpointDescription: (name) => `Are you sure you want to delete "${name}"? This action cannot be undone.`,
    dragToReorder: (name) => `Drag to reorder endpoint ${name}`,
    duplicateEndpoint: (name) => `Duplicate endpoint ${name}`,
    editEndpoint: (name) => `Edit endpoint ${name}`,
    keepStoredKey: "Leave blank to keep the existing stored key.",
    models: "Models",
    name: "Name",
    nameRequired: "Name is required",
    namePlaceholder: "e.g. OpenAI Production",
    none: "None",
  },
  endpointsData: {
    created: "Endpoint created",
    createFailed: "Failed to create endpoint",
    deleted: "Endpoint deleted",
    deleteFailed: "Failed to delete endpoint",
    duplicatedAs: (name) => `Endpoint duplicated as ${name}`,
    duplicateFailed: "Failed to duplicate endpoint",
    loadFailed: "Failed to load endpoints",
    reorderedFailed: "Failed to reorder endpoints",
    updated: "Endpoint updated",
    updateFailed: "Failed to update endpoint",
  },
  modelsPage: {
    countDescription: (count) => `${count} model configurations`,
    newModel: "New Model",
    searchModels: "Search models...",
    title: "Models",
  },
  modelsUi: {
    addTarget: "Add Target",
    allNativeModelsIncluded: "All native models for this API family are already included.",
    deleteModel: "Delete Model",
    deleteModelDescription: (name) => `Are you sure you want to delete "${name}"? This will also delete all associated endpoints.`,
    displayNameOptional: "Display Name",
    editModel: "Edit Model",
    modelId: "Model ID",
    modelIdPlaceholder: "e.g. gpt-4o",
    noNativeModelsForFamily: (apiFamily) => `No native models available for the ${apiFamily} API family yet. Configure targets later on /models/:id/proxy.`,
    noProxyTargetsSelected: "No proxy targets selected yet.",
    optionalFriendlyName: "Optional friendly name",
    priority: (value) => `Priority ${value}`,
    proxyTargetsDescriptionPrimary: "Requests try these native targets in order and stop at the first available target.",
    proxyTargetsDescriptionSecondary: "You can create this proxy now and configure targets later on /models/:id/proxy.",
    remainingNativeTargets: (count) => `${count} more native targets available.`,
    routingTypeDescription: "Turn this model on or off",
    save: "Save",
    strategyNotConfigured: "Strategy not configured",
    targetMoveDown: (id) => `Move target ${id} down`,
    targetMoveUp: (id) => `Move target ${id} up`,
    targetRemove: (id) => `Remove target ${id}`,
    viewModelDetails: (name) => `View model details for ${name}`,
    noModelsMatchSearch: "No models match search",
    noModelsConfigured: "No models configured",
    tryDifferentModelNameOrId: "Try a different model name or ID",
    createFirstModel: "Create your first model to get started",
    activeConnections: (active, total) => `${active}/${total} active`,
    successLabel: "success",
    requestsShort: "req",
    spendShort: "spend",
    unknownVendor: "Unknown vendor",
    noProxyTargets: "No proxy targets",
    targetsFirst: (count, first) => `${count} targets · ${first} first`,
    modelCount: (count) => `${count} ${count === "1" ? "model" : "models"}`,
  },
  modelsData: {
    created: "Model created",
    deleted: "Model deleted",
    deleteFailed: "Failed to delete model",
    fetchFailed: "Failed to fetch data",
    saveFailed: "Failed to save model",
    selectApiFamily: "Please select an API family",
    selectLoadbalanceStrategy: "Please select a loadbalance strategy for native models",
    selectVendor: "Please select a vendor",
    updated: "Model updated",
  },
  pricingTemplatesUi: {
    actions: "Actions",
    addTemplate: "Add Template",
    close: "Close",
    currency: "Currency",
    deletePricingTemplate: "Delete Pricing Template",
    deletePricingTemplateDescription: (name) => `Are you sure you want to delete the template "${name}"?`,
    deletePricingTemplateInUse: (count) => `Cannot delete this template because it is currently used by ${count} connection(s).`,
    description: "Manage reusable pricing templates for models and endpoints",
    endpoint: "Endpoint",
    input: "Input",
    model: "Model",
    noTemplatesConfigured: "No pricing templates configured.",
    output: "Output",
    profileScopedSettings: "Profile-scoped settings",
    scopeCallout: (profileLabel) => `Changes here affect ${profileLabel} and its runtime traffic.`,
    tableTitle: "Pricing Templates",
    templateUsage: "Template Usage",
    templateUsageDescription: (name) => `Connections currently using the "${name}" template.`,
    templateUnused: "This template is not currently used by any connections.",
    title: "Pricing Templates",
    unnamed: "Unnamed",
    viewUsage: "View usage",
  },
  pricingTemplatesData: {
    cacheCreationNonNegative: "Cache creation price must be a non-negative number",
    cachedInputNonNegative: "Cached input price must be a non-negative number",
    changedWhileEditing: "This pricing template changed while you were editing it. Reopen the dialog and try again.",
    created: "Pricing template created",
    deleted: "Pricing template deleted",
    deleteFailed: "Failed to delete pricing template",
    endpointWithId: (id) => `Endpoint #${id}`,
    inUseCannotDelete: "Cannot delete template because it is in use",
    inputNonNegative: "Input price must be a non-negative number",
    invalidCurrency: "Pricing currency must be a valid 3-letter code (for example, USD)",
    loadFailed: "Failed to load pricing templates",
    loadSingleFailed: "Failed to load pricing template",
    loadUsageFailed: "Failed to load template usage",
    nameRequired: "Name is required",
    unknownModel: "Unknown model",
    outputNonNegative: "Output price must be a non-negative number",
    reasoningNonNegative: "Reasoning price must be a non-negative number",
    saveFailed: "Failed to save pricing template",
    updated: "Pricing template updated",
  },
  proxyApiKeys: {
    actions: "Actions",
    active: "Active",
    apiKey: "API key",
    authenticationOff: "Authentication Off",
    authenticationOn: "Authentication On",
    authenticationUnavailable: "Authentication Unavailable",
    copyKey: "Copy key",
    createDescription: "Add a name and optional note, then create a new client credential.",
    createKey: "Create key",
    createProxyKey: "Create proxy key",
    creating: "Creating...",
    created: "Created",
    deleteKey: "Delete key",
    deleteProxyApiKey: "Delete Proxy API Key",
    deleteProxyApiKeyDescription: (name, prefix) => `Delete the key "${name}"? Requests using this secret will stop working immediately. Confirm the prefix ${prefix} before continuing.`,
    deleteProxyKeyAria: (name) => `Delete proxy key ${name}`,
    description:
      "Manage machine credentials used by upstream clients to access the Prism proxy. Applies to all profiles.",
    disabled: "Disabled",
    editDescription: "Update the stored name, note, and active state for this issued key. Rotating the secret is a separate action.",
    editProxyApiKey: "Edit Proxy API Key",
    editProxyKeyAria: (name) => `Edit proxy key ${name}`,
    issuedKeys: "Issued keys",
    keyCount: (count) => `${count} key${count === "1" ? "" : "s"}`,
    keyLimitReached: "Key limit reached",
    keysPreparedDescription: "Keys are prepared but not enforced until authentication is enabled.",
    keysProtectedDescription: "Keys are active for protected proxy traffic.",
    keysUsed: (used, limit) => `${used} / ${limit} keys used`,
    lastIp: "Last IP",
    lastUsed: "Last used",
    listDescription: "Edit metadata, rotate, or delete keys directly from the list below.",
    name: "Name",
    nameNote: "Name / note",
    namePlaceholder: "Production client",
    newSecret: "New secret",
    newSecretDescription: "This full key is shown once. Store it before leaving the page.",
    noInternalNote: "No internal note.",
    noProxyKeysCreated: "No proxy keys created yet.",
    notes: "Notes",
    notesPlaceholder: "Used by the main website",
    operation: "Operation",
    preview: "Preview",
    rotateProxyKeyAria: (name) => `Rotate proxy key ${name}`,
    slotsRemaining: (remaining) => `${remaining} slot${remaining === "1" ? "" : "s"} remaining.`,
    title: "Proxy API Keys",
    never: "Never",
    unknown: "Unknown",
    updated: "Updated",
  },
  proxyApiKeysData: {
    created: "Proxy API key created",
    createFailed: "Failed to create proxy API key",
    deleted: "Proxy API key deleted",
    deleteFailed: "Failed to delete proxy API key",
    keyNameRequired: "Key name is required",
    loadAuthStatusFailed: "Failed to load authentication status",
    loadKeysFailed: "Failed to load proxy API keys",
    maxKeysReached: (limit) => `Maximum ${limit} proxy API keys reached`,
    rotated: "Proxy API key rotated",
    rotateFailed: "Failed to rotate proxy API key",
    settingsUnavailable: "Authentication settings are unavailable",
    updated: "Proxy API key updated",
    updateFailed: "Failed to update proxy API key",
  },
  requestLogs: {
    allColumns: "All columns",
    allConnections: "All connections",
    allEndpoints: "All endpoints",
    allModels: "All models",
    allStatuses: "All statuses",
    any: "Any",
    anyLatency: "Any latency",
    anyOutcome: "Any outcome",
    audit: "Audit",
    billableOnly: "Billable only",
    cacheCreation: "Cache creation",
    cacheRead: "Cache read",
    compact: "Compact",
    connection: "Connection",
    detailDescription: "Review request metadata, routing, tokens, costs, and captured upstream payloads.",
    endpoint: "Endpoint",
    fiveHundredsOnly: "5xx only",
    fourHundredsOnly: "4xx only",
    last6Hours: "Last 6 hours",
    last24Hours: "Last 24 hours",
    last30Days: "Last 30 days",
    last7Days: "Last 7 days",
    lastHour: "Last hour",
    latency: "Latency",
    latencyFast: "< 500ms",
    latencyNormal: "500ms-2s",
    latencySlow: "2s-5s",
    latencyVerySlow: "> 5s",
    localRefinement: "Local refinement",
    loadFailed: "Failed to load request logs",
    max: "Max",
    min: "Min",
    model: "Model",
    nonStreaming: "Non-streaming",
    outcome: "Outcome",
    overview: "Overview",
    pricedOnly: "Priced only",
    reasoning: "Reasoning",
    refreshRequestLogs: "Refresh request logs",
    requestId: "Request ID",
    requestTitle: (id) => `Request #${id}`,
    requestNotFound: "Request Not Found",
    requestNotFoundDescription: (id) => `Request #${id} could not be found. It may have been deleted or you might not have access to it.`,
    requestLogsAllTime: "All time",
    requestLogsDescription: "Browse and investigate proxied requests",
    requestLogsTitle: "Request Logs",
    noCaptured: (title) => `No ${title.toLowerCase()} captured.`,
    noRequestLogsMatchSlice: "No request logs match this slice",
    requestBody: "Request",
    requestHeaders: "Request headers",
    search: "Search",
    searchPlaceholder: "model, vendor, path, or error",
    relaxScope: "Relax the scope or clear local refinements to widen the investigation surface.",
    returnToRequestList: "Return to request list",
    response: (status) => `Response (${status})`,
    resultsRange: (start, end, total) => `${start}-${end} of ${total}`,
    rowsPerPage: "rows per page",
    specialTokens: "Special tokens",
    status: "Status",
    stream: "Stream",
    streaming: "Streaming",
    technicalInspection: "Technical inspection",
    requestDetails: "Request details",
    requestedModel: "Requested model",
    proxyOrigin: "Proxy origin",
    resolvedTarget: "Resolved target",
    time: "Time",
    totalCost: "Total cost",
    totalTokens: "Total tokens",
    timestamp: "Timestamp",
    errorDetail: "Error detail",
    ingressRequestId: "Ingress request ID",
    attemptNumber: "Attempt number",
    providerCorrelationId: "Provider correlation ID",
    formattedForReadability: "Captured upstream failure detail, formatted for readability.",
    capturedFailureDetail: "Captured upstream failure detail.",
    copy: "Copy",
    path: "Path",
    routingContext: "Routing context",
    tokenUsage: "Token usage",
    costBreakdown: "Cost breakdown",
    input: "Input",
    output: "Output",
    total: "Total",
    priced: "Priced",
    billable: "Billable",
    yes: "Yes",
    no: "No",
    whyUnpriced: "Why unpriced",
    baseUrl: "Base URL",
    auditCapture: "Audit capture",
    auditCaptureUnavailable: "Audit capture unavailable",
    auditCaptureDisabledForVendor: "Audit logging may be disabled for this vendor.",
    auditLoadFailedTitle: "Audit detail load failed",
    auditLoadFailed: "Failed to load audit details after multiple attempts.",
    noAuditRecords: "No audit records found for this request.",
    timeRange: "Time range",
    tokenRange: "Token range",
    tokens: "Tokens",
    triage: "Triage",
    view: "View",
    spend: "Cost",
    viewRequestInLogs: "View in Request Logs",
    viewingRequest: (id) => `Viewing request #${id}`,
    exit: "Exit",
    zeroResults: "0 results",
  },
  requestLogsDetail: {
    connectionNotFound: "Connection not found — it may have been deleted",
    copyFailed: (label) => `Failed to copy ${label}`,
    copied: (label) => `Copied ${label}`,
  },
  shell: {
    activate: "Activate",
    activateProfile: "Activate profile",
    activating: "Activating...",
    activeRuntime: (name) => `Active runtime: ${name}`,
    aligned: "Aligned",
    collapseSidebar: "Collapse sidebar",
    closeSidebar: "Close sidebar",
    expandSidebar: "Expand sidebar",
    logoutFailed: "Failed to sign out",
    mismatch: "Mismatch",
    mismatchWarning: (selected, active) =>
      `You're viewing ${selected}, but runtime traffic is served by ${active}.`,
    openSidebar: "Open sidebar",
    out: "Out",
    primaryNavigation: "Primary navigation",
    profile: "Profile:",
    profileRuntime: "Profile runtime",
    runtime: "Runtime",
    signOut: "Sign out",
    viewing: "Viewing",
  },
  statistics: {
    addLine: "Add Line",
    averageRpm: "Average RPM",
    adjustFiltersOrTimeRange: "Try adjusting your filters or time range.",
    aggregation: "Aggregation",
    all: "All",
    allConnections: "All Connections",
    allModels: "All Models",
    allRows: "All rows",
    anyError: "Any error",
    availability: "Availability",
    byDay: "By Day",
    byHour: "By Hour",
    cacheHitRate: "Cache Hit Rate",
    cachedRows: (count) => `${count} cached rows`,
    clearFilters: "Clear Filters",
    connection: "Connection",
    costOverviewTitle: "Cost Overview",
    costByBucket: "Cost by Bucket",
    costComponentsBy: (groupBy) => `Cost Components by ${groupBy}`,
    costEfficiencyScatter: "Cost Efficiency Scatter",
    costInsights: "Cost Insights",
    currentRpm: "Current RPM",
    debug: "Debug",
    errors: "Errors",
    fourxxRate: "4xx Rate",
    fivexxRate: "5xx Rate",
    filters: "Filters",
    filtersApplyToAllSpending: "Filters apply to all spending metrics and breakdowns below.",
    from: "From",
    group: "Group",
    groupBy: "Group By",
    health: "Health",
    highestOneMinuteThroughput: "Highest 1-minute throughput",
    highestSpend: "Highest Spend",
    input: "Input",
    inputOutputSpecial: "Input + output + special tokens",
    noSpendingDataFound: "No spending data found",
    loadingThroughputData: "Loading throughput data...",
    latencyDistribution: "Latency Distribution",
    latencyPercentiles: "Latency Percentiles",
    mostRecentOneMinuteBucket: "Most recent 1-minute bucket",
    mostFrequentErrorSignatures: "Most frequent error signatures for this filter set.",
    noCostRecordsFound: "No cost records found.",
    operationsDescription: "Operational metrics and spending analytics",
    operationsTab: "Operations",
    noDataPointsAvailable: "No data points available",
    noErrorSignaturesFound: "No error signatures found.",
    noHttpErrorsInSlice: "No HTTP errors in this slice.",
    noRequestsFound: "No requests found.",
    noThroughputDataAvailable: "No throughput data available",
    output: "Output",
    peakRpm: "Peak RPM",
    p95Latency: "P95 Latency",
    p99Latency: "P99 Latency",
    percentTotal: "% Total",
    pricedPercent: "Priced %",
    vendorLabel: "Vendor",
    refreshThroughputStatistics: "Refresh throughput statistics",
    refreshOperationsStatistics: "Refresh operations statistics",
    refreshSpendingStatistics: "Refresh spending statistics",
    refreshUsageStatistics: "Refresh usage statistics",
    reset: "Reset",
    customRange: "Custom Range",
    lastHour: "Last 1 Hour",
    last6Hours: "Last 6 Hours",
    last24Hours: "Last 24 Hours",
    last7Days: "Last 7 Days",
    last30Days: "Last 30 Days",
    allTime: "All Time",
    today: "Today",
    day: "Day",
    week: "Week",
    month: "Month",
    endpointGroup: "Endpoint",
    endpointStatisticsTitle: "Endpoint Statistics",
    exportSnapshotJson: "Export snapshot JSON",
    lineLimitReached: "You can compare up to 9 model lines at once.",
    linesSelected: (count, max) => `${count} / ${max}`,
    linesToDisplay: "Lines to Display",
    modelGroup: "Model",
    modelStatisticsTitle: "Model Statistics",
    modelEndpointGroup: "Model + Endpoint",
    noEndpointStatisticsDescription: "Endpoint rollups will appear here after traffic is processed.",
    noEndpointStatisticsTitle: "No endpoint statistics in this time range",
    noModelStatisticsDescription: "Model rollups will appear here after traffic is processed.",
    noModelStatisticsTitle: "No model statistics in this time range",
    noProxyApiKeyUsageDescription: "Runtime-auth usage will appear here after proxy API keys are used.",
    noProxyApiKeyUsageTitle: "No proxy API key usage in this time range",
    openPricingTemplates: "Open Pricing Templates",
    overviewTitle: "Overview",
    pricingDataMissingDescription: "Attach pricing templates to connections to unlock cost coverage on the statistics page.",
    pricingDataMissingTitle: "Pricing data is missing for this time range",
    proxyApiKey: "Proxy API Key",
    proxyApiKeyStatisticsTitle: "Proxy API Key Statistics",
    removeLine: (label) => `Remove line ${label}`,
    previousPage: "Previous Page",
    nextPage: "Next Page",
    requestTrendsTitle: "Request Trends",
    requestsInWindow: (count) => `${count} reqs in window`,
    requestsTab: "Requests",
    requests: "Requests",
    requestsPerMinuteOverTime: "Requests Per Minute (RPM) Over Time",
    rows: "Rows",
    selectModelLinePlaceholder: "Choose a model line",
    serviceHealthTitle: "Service Health",
    slow: "Slow",
    slowestRequests: "Slowest requests by latency in current filtered slice.",
    spend: "Spend",
    spendingDescription: "Operational metrics and spending analytics",
    spendingTab: "Spending",
    spendingBreakdown: "Spending Breakdown",
    specialTokenCoverageVisibleRows: "Special Token Coverage (visible rows)",
    cachedCaptured: "Cached captured",
    cachedPrefix: "Cached",
    connectionId: "Connection ID",
    costly: "Costly",
    currency: "Currency",
    dollarsPerMillionTokens: "$ / 1M tokens",
    dollarsPerRequest: "$ / Request",
    modelId: "Model ID",
    noDataAvailable: "No data available",
    reasoningCaptured: "Reasoning captured",
    anySpecialCaptured: "Any special captured",
    failedCount: (count) => `${count} failed`,
    failedToLoadEndpointModelStatistics: "Failed to load endpoint model statistics",
    failedToLoadUsageStatistics: "Failed to load usage statistics",
    healthStatusDegraded: "Degraded",
    healthStatusDown: "Down",
    healthStatusIdle: "Idle",
    healthStatusOk: "OK",
    heatmapLegendLessAvailability: "Lower availability",
    heatmapLegendMoreAvailability: "Higher availability",
    latest: "Latest",
    loadingEndpointModelStatistics: "Loading model usage…",
    noTokenUsage: "No token usage",
    oldest: "Oldest",
    serviceHealthIntervalHours: (count) => (count === 1 ? "1 hour" : `${count} hours`),
    serviceHealthIntervalMinutes: (count) => (count === 1 ? "1 minute" : `${count} minutes`),
    successful: (count) => `${count} successful`,
    successfulCount: (count) => `${count} successful`,
    serviceHealthWindowDays: (count: number) => (count === 1 ? "Last day" : `Last ${count} days`),
    successOnly: "Successful only",
    successRate: "Success Rate",
    specialTokens: "Special Tokens",
    statisticsDescription: "One request-based usage snapshot across requests, tokens, cost, endpoints, models, and proxy API keys.",
    statisticsTitle: "Usage Statistics",
    tokenTypeBreakdownTitle: "Token Type Breakdown",
    tokenUsageTrendsTitle: "Token Usage Trends",
    topHttpErrors: "Top HTTP Errors",
    timeWindow: "Time Range",
    timeWindowTotal: (seconds) => `${seconds}s total`,
    to: "To",
    totalSpend: "Total Spend",
    totalTokens: "Total Tokens",
    throughputExplanation:
      "Each data point represents a 1-minute time bucket. RPM matches the requests recorded in that minute, and Average RPM normalizes the selected window to requests per minute.",
    throughputTab: "Throughput",
    tokens: "Tokens",
    tokenThroughput: "Token Throughput",
    topN: "Top N",
    topEndpointsByCost: "Top Endpoints by Requests",
    topModelsByCost: "Top Models by Requests",
    totalRequests: (count) => `${count} total requests`,
    updated: "Updated",
    unpriced: (count) => `${count} unpriced`,
    unpricedBreakdown: "Unpriced Breakdown",
    unknownProxyApiKey: "Unknown proxy API key",
    usageAndCost: "Usage & Cost",
    usageStatisticsPagePlaceholder: "Usage statistics page placeholder",
    performance: "Performance",
    requestOutcomeOverTime: "Request Outcome Over Time",
  },
  theme: {
    changeTheme: "Change theme",
    dark: "Dark",
    light: "Light",
    system: "System",
  },
};
