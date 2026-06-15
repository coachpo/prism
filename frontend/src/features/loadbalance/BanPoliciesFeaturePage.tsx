import { useState } from "react"
import { Activity, RefreshCw, Search } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Field, FieldDescription, FieldGroup, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { LoadbalanceEventDetailSheet } from "@/components/loadbalance/LoadbalanceEventDetailSheet"
import { LoadbalanceEventsTable } from "@/components/loadbalance/LoadbalanceEventsTable"
import { DeleteLoadbalanceStrategyDialog } from "@/pages/loadbalance-strategies/DeleteLoadbalanceStrategyDialog"
import { LoadbalanceStrategiesTable } from "@/pages/loadbalance-strategies/LoadbalanceStrategiesTable"
import { useProfileContext } from "@/context/ProfileContext"
import { useLocale } from "@/i18n/useLocale"
import { useTimezone } from "@/hooks/useTimezone"
import { api } from "@/lib/api"
import type { LoadbalanceCurrentStateItem, LoadbalanceEvent } from "@/lib/types"
import {
  OperatorCallout,
  OperatorEmptyState,
  OperatorLoadingState,
  OperatorPageHeader,
  OperatorPageShell,
  OperatorSectionCard,
  OperatorTypeBadge,
} from "@/shared/design-system"
import { BanPolicyDialog } from "./BanPolicyDialog"
import { useBanPoliciesFeatureData } from "./useBanPoliciesFeatureData"

const EVENTS_PAGE_SIZE = 25

export function BanPoliciesFeaturePage() {
  const { selectedProfile, revision } = useProfileContext()
  const { messages } = useLocale()
  const data = useBanPoliciesFeatureData(revision)
  const selectedProfileLabel = selectedProfile ? `${selectedProfile.name} (#${selectedProfile.id})` : messages.loadbalanceStrategiesPage.selectedProfileFallback

  return (
    <OperatorPageShell data-testid="ban-policies-feature-page">
      <OperatorPageHeader title="Ban Policy Strategies" description={messages.loadbalanceStrategiesPage.description} />
      <OperatorCallout
        action={<OperatorTypeBadge intent="warning" label={messages.settingsPage.profileScopedSettings} preserveLabel />}
        description={messages.loadbalanceStrategiesPage.scopeCallout(selectedProfileLabel)}
        intent="warning"
      />

      <Tabs defaultValue="strategies" className="gap-5">
        <TabsList>
          <TabsTrigger value="strategies">Strategies</TabsTrigger>
          <TabsTrigger value="current-state">Current State</TabsTrigger>
          <TabsTrigger value="events">Events</TabsTrigger>
        </TabsList>
        <TabsContent value="strategies" className="flex flex-col gap-5">
          <LoadbalanceStrategiesTable loadbalanceStrategies={data.strategies} loadbalanceStrategiesLoading={data.strategiesLoading} loadbalanceStrategyDefaultsCreating={data.defaultsCreating} loadbalanceStrategyPreparingEditId={data.preparingEditId} onCreate={data.openCreate} onCreateDefaults={data.createDefaults} onDelete={data.openDelete} onEdit={data.openEdit} />
        </TabsContent>
        <TabsContent value="current-state"><BanPolicyCurrentStatePanel revision={revision} /></TabsContent>
        <TabsContent value="events"><BanPolicyEventsPanel revision={revision} /></TabsContent>
      </Tabs>

      <BanPolicyDialog editingStrategy={data.editingStrategy} initialValues={data.formValues} onClose={() => data.setDialogOpen(false)} onOpenChange={data.setDialogOpen} onSave={data.save} open={data.dialogOpen} saving={data.saving} />
      <DeleteLoadbalanceStrategyDialog deleteLoadbalanceStrategyConfirm={data.deleteConfirm} displayedDeleteLoadbalanceStrategyConfirm={data.displayDelete} loadbalanceStrategyDeleting={data.deleting} onClose={data.closeDelete} onDelete={data.deleteStrategy} open={data.deleteConfirm !== null} />
    </OperatorPageShell>
  )
}

function BanPolicyCurrentStatePanel({ revision }: { revision: number }) {
  const { formatNumber, messages } = useLocale()
  const { format: formatTime } = useTimezone()
  const [modelConfigId, setModelConfigId] = useState("")
  const [states, setStates] = useState<LoadbalanceCurrentStateItem[]>([])
  const [loading, setLoading] = useState(false)
  const [searched, setSearched] = useState(false)
  const [resettingConnectionId, setResettingConnectionId] = useState<number | null>(null)
  const loadState = async () => {
    const parsedId = Number(modelConfigId)
    if (!Number.isInteger(parsedId) || parsedId <= 0) return
    setLoading(true)
    setSearched(true)
    try {
      const response = await api.loadbalance.listCurrentState({ model_config_id: parsedId })
      setStates(response.items)
    } finally {
      setLoading(false)
    }
  }
  const resetState = async (connectionId: number) => {
    setResettingConnectionId(connectionId)
    try {
      await api.loadbalance.resetCurrentState(connectionId)
      setStates((current) => current.filter((item) => item.connection_id !== connectionId))
    } finally {
      setResettingConnectionId(null)
    }
  }

  return (
    <OperatorSectionCard
      title="Current Ban Policy State"
      description={`Inspect retry-window and ban state for one model config ID in the selected profile. Refresh after switching profiles; revision ${revision}.`}
      contentClassName="flex flex-col gap-4"
    >
      <FieldGroup className="gap-4">
        <Field>
          <FieldLabel htmlFor="current-state-model-config-id">Model config ID</FieldLabel>
          <FieldDescription>Use the model row ID from the selected profile.</FieldDescription>
          <div className="flex flex-col gap-2 sm:flex-row">
            <Input
              id="current-state-model-config-id"
              inputMode="numeric"
              value={modelConfigId}
              onChange={(event) => setModelConfigId(event.target.value)}
              placeholder="42"
            />
            <Button
              type="button"
              onClick={() => void loadState()}
              disabled={loading || !modelConfigId.trim()}
            >
              <Search data-icon="inline-start" />
              Load State
            </Button>
          </div>
        </Field>
      </FieldGroup>

      {!searched ? (
        <OperatorEmptyState
          icon={<Activity />}
          title="No model selected"
          description="Enter a model config ID to view current Ban Policy state."
        />
      ) : null}

      {loading ? (
        <OperatorLoadingState
          title="Loading Ban Policy state"
          description="Checking the selected model config across current connection state."
        />
      ) : null}

      {searched && !loading && states.length === 0 ? (
        <OperatorEmptyState
          icon={<Activity />}
          title="No current state"
          description="No current Ban Policy state is recorded for this model."
        />
      ) : null}

      {states.length > 0 ? (
        <div className="overflow-hidden rounded-xl border border-outline-variant">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Connection</TableHead>
                <TableHead>State</TableHead>
                <TableHead>Cycle attempts</TableHead>
                <TableHead>Cumulative attempts</TableHead>
                <TableHead>Next retry</TableHead>
                <TableHead>Ban mode</TableHead>
                <TableHead className="text-right">Actions</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {states.map((state) => (
                <TableRow key={state.connection_id}>
                  <TableCell className="font-mono text-xs">#{state.connection_id}</TableCell>
                  <TableCell><OperatorTypeBadge label={state.state} /></TableCell>
                  <TableCell>{formatNumber(state.cycle_retry_attempts)}</TableCell>
                  <TableCell>{formatNumber(state.cumulative_retry_attempts)}</TableCell>
                  <TableCell>
                    {state.next_retry_at
                      ? formatTime(state.next_retry_at, { month: "short", day: "numeric", hour: "numeric", minute: "2-digit" })
                      : messages.common.notApplicable}
                  </TableCell>
                  <TableCell>{state.ban_mode}</TableCell>
                  <TableCell className="text-right">
                    <Button
                      type="button"
                      variant="outline"
                      size="sm"
                      disabled={resettingConnectionId === state.connection_id}
                      onClick={() => void resetState(state.connection_id)}
                    >
                      {messages.modelDetail.resetBanPolicyState}
                    </Button>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      ) : null}
    </OperatorSectionCard>
  )
}

function BanPolicyEventsPanel({ revision }: { revision: number }) {
  const [modelId, setModelId] = useState("")
  const [events, setEvents] = useState<LoadbalanceEvent[]>([])
  const [total, setTotal] = useState(0)
  const [offset, setOffset] = useState(0)
  const [loading, setLoading] = useState(false)
  const [searched, setSearched] = useState(false)
  const [selectedEventId, setSelectedEventId] = useState<number | null>(null)
  const loadEvents = async (nextOffset = 0) => {
    if (!modelId.trim()) return
    setLoading(true)
    setSearched(true)
    try {
      const response = await api.loadbalance.listEvents({ model_id: modelId.trim(), limit: EVENTS_PAGE_SIZE, offset: nextOffset })
      setEvents(response.items)
      setTotal(response.total)
      setOffset(nextOffset)
    } finally {
      setLoading(false)
    }
  }
  return (
    <OperatorSectionCard
      title="Ban Policy Events"
      description={`Review retry scheduled, retry exhausted, banned, unbanned, recovered, and admission rejected events for one model ID. Selected profile revision ${revision}.`}
      contentClassName="flex flex-col gap-4"
    >
      <FieldGroup className="gap-4">
        <Field>
          <FieldLabel htmlFor="events-model-id">Model ID</FieldLabel>
          <FieldDescription>Use the public model ID whose Ban Policy history you want to inspect.</FieldDescription>
          <div className="flex flex-col gap-2 sm:flex-row">
            <Input
              id="events-model-id"
              value={modelId}
              onChange={(event) => setModelId(event.target.value)}
              placeholder="gpt-4o"
            />
            <Button
              type="button"
              onClick={() => void loadEvents(0)}
              disabled={loading || !modelId.trim()}
            >
              <RefreshCw data-icon="inline-start" />
              Load Events
            </Button>
          </div>
        </Field>
      </FieldGroup>

      {!searched ? (
        <OperatorEmptyState
          icon={<Activity />}
          title="No model selected"
          description="Enter a model ID to view Ban Policy event history."
        />
      ) : null}

      {searched ? (
        <LoadbalanceEventsTable
          events={events}
          loading={loading}
          total={total}
          offset={offset}
          limit={EVENTS_PAGE_SIZE}
          onSelectEvent={setSelectedEventId}
          onPreviousPage={() => void loadEvents(Math.max(0, offset - EVENTS_PAGE_SIZE))}
          onNextPage={() => void loadEvents(offset + EVENTS_PAGE_SIZE)}
        />
      ) : null}

      <LoadbalanceEventDetailSheet eventId={selectedEventId} onClose={() => setSelectedEventId(null)} />
    </OperatorSectionCard>
  )
}

export default BanPoliciesFeaturePage
