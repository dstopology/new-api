/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import {
  useCallback,
  useMemo,
  useState,
  type ChangeEvent,
  type ElementType,
  type ReactNode,
} from 'react'
import { useQuery } from '@tanstack/react-query'
import {
  AlertTriangle,
  BarChart3,
  CircleDollarSign,
  Coins,
  CreditCard,
  RefreshCw,
  Search,
  SlidersHorizontal,
  TrendingUp,
  WalletCards,
  Zap,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import {
  formatNumber,
  formatTimestampForInput,
  formatTimestampToDate,
  parseTimestampFromInput,
} from '@/lib/format'
import { cn } from '@/lib/utils'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  NativeSelect,
  NativeSelectOption,
} from '@/components/ui/native-select'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { SectionPageLayout } from '@/components/layout'
import { FadeIn } from '@/components/page-transition'
import { getProfitOverview } from './api'
import type {
  ProfitChannelItem,
  ProfitModelItem,
  ProfitOverview,
  ProfitQueryParams,
  ProfitTopUpItem,
  ProfitTrendItem,
} from './types'

type ProfitFilterDraft = {
  start_timestamp: number
  end_timestamp: number
  channel_type: string
  channel_id: string
  model_name: string
  group: string
  payment_provider: string
  payment_method: string
}

const EMPTY_OVERVIEW: ProfitOverview = {
  summary: {
    start_timestamp: 0,
    end_timestamp: 0,
    topup_amount: 0,
    epay_wx_amount: 0,
    revenue_usd: 0,
    estimated_cost_usd: 0,
    profit_usd: 0,
    profit_rate: 0,
    request_count: 0,
    failed_count: 0,
    topup_count: 0,
    avg_topup_amount: 0,
    truncated: false,
    truncated_limit: 0,
  },
  trends: [],
  channels: [],
  models: [],
  topups: [],
}

const QUICK_RANGES = [
  { days: 1, label: 'Last 24 Hours' },
  { days: 7, label: 'Last 7 Days' },
  { days: 30, label: 'Last 30 Days' },
] as const

const CHANNEL_PROVIDERS = [
  { value: '', label: 'All Providers' },
  { value: '1', label: 'OpenAI' },
  { value: '14', label: 'Anthropic' },
  { value: '24', label: 'Gemini' },
] as const

const PAYMENT_PROVIDERS = [
  { value: '', label: 'All Payment Providers' },
  { value: 'epay', label: 'Epay' },
  { value: 'stripe', label: 'Stripe' },
  { value: 'creem', label: 'Creem' },
  { value: 'waffo', label: 'Waffo' },
  { value: 'waffo_pancake', label: 'Waffo Pancake' },
] as const

const PAYMENT_METHODS = [
  { value: '', label: 'All Payment Methods' },
  { value: 'wxpay', label: 'WeChat Pay' },
  { value: 'alipay', label: 'Alipay' },
  { value: 'stripe', label: 'Stripe' },
  { value: 'creem', label: 'Creem' },
  { value: 'waffo', label: 'Waffo' },
  { value: 'waffo_pancake', label: 'Waffo Pancake' },
] as const

function getRange(days: number) {
  const end = Math.floor(Date.now() / 1000)
  return {
    start_timestamp: end - days * 24 * 60 * 60,
    end_timestamp: end,
  }
}

function createDefaultDraft(): ProfitFilterDraft {
  return {
    ...getRange(7),
    channel_type: '',
    channel_id: '',
    model_name: '',
    group: '',
    payment_provider: '',
    payment_method: '',
  }
}

function toQueryParams(draft: ProfitFilterDraft): ProfitQueryParams {
  const start = Math.min(draft.start_timestamp, draft.end_timestamp)
  const end = Math.max(draft.start_timestamp, draft.end_timestamp)
  const channelId = Number(draft.channel_id)
  const channelType = Number(draft.channel_type)

  return {
    start_timestamp: start,
    end_timestamp: end,
    ...(Number.isFinite(channelType) && channelType > 0
      ? { channel_type: channelType }
      : {}),
    ...(Number.isFinite(channelId) && channelId > 0
      ? { channel_id: channelId }
      : {}),
    ...(draft.model_name.trim()
      ? { model_name: draft.model_name.trim() }
      : {}),
    ...(draft.group.trim() ? { group: draft.group.trim() } : {}),
    ...(draft.payment_provider
      ? { payment_provider: draft.payment_provider }
      : {}),
    ...(draft.payment_method ? { payment_method: draft.payment_method } : {}),
  }
}

function formatUsd(value: number | null | undefined): string {
  if (value == null || Number.isNaN(value)) return '-'
  return Intl.NumberFormat(undefined, {
    style: 'currency',
    currency: 'USD',
    maximumFractionDigits: Math.abs(value) < 1 ? 4 : 2,
  }).format(value)
}

function formatAmount(value: number | null | undefined): string {
  if (value == null || Number.isNaN(value)) return '-'
  return formatNumber(value)
}

function formatRate(value: number | null | undefined): string {
  if (value == null || Number.isNaN(value)) return '-'
  return `${formatNumber(value)}%`
}

function formatCostRatio(value: number | null | undefined): string {
  if (value == null || Number.isNaN(value) || value <= 0) return '-'
  return `${formatNumber(value)}x`
}

function formatTokens(promptTokens: number, completionTokens: number): string {
  return formatNumber((promptTokens || 0) + (completionTokens || 0))
}

function paymentLabel(value: string, t: (key: string) => string): string {
  if (!value) return '-'
  const map: Record<string, string> = {
    epay: 'Epay',
    stripe: 'Stripe',
    creem: 'Creem',
    waffo: 'Waffo',
    waffo_pancake: 'Waffo Pancake',
    wxpay: 'WeChat Pay',
    alipay: 'Alipay',
  }
  return t(map[value] ?? value)
}

function channelProviderLabel(
  value: number | string,
  t: (key: string) => string
): string {
  const item = CHANNEL_PROVIDERS.find(
    (provider) => provider.value === String(value)
  )
  return item ? t(item.label) : String(value)
}

function ProfitPanel(props: {
  title: ReactNode
  description?: ReactNode
  children: ReactNode
  className?: string
}) {
  return (
    <section
      className={cn('overflow-hidden rounded-lg border bg-card', props.className)}
    >
      <div className='border-b px-3 py-2.5 sm:px-4'>
        <div className='text-sm font-semibold'>{props.title}</div>
        {props.description != null && (
          <div className='text-muted-foreground mt-1 text-xs'>
            {props.description}
          </div>
        )}
      </div>
      <div className='p-3 sm:p-4'>{props.children}</div>
    </section>
  )
}

function MetricCard(props: {
  title: string
  value: string
  desc: string
  icon: ElementType
  loading?: boolean
  tone?: 'default' | 'success' | 'warning' | 'danger'
}) {
  const Icon = props.icon
  return (
    <div className='rounded-lg border px-3 py-2.5'>
      <div className='flex items-center gap-2'>
        <Icon
          className={cn(
            'size-3.5 shrink-0',
            props.tone === 'success'
              ? 'text-success'
              : props.tone === 'warning'
                ? 'text-warning'
                : props.tone === 'danger'
                  ? 'text-destructive'
                  : 'text-muted-foreground/70'
          )}
        />
        <div className='text-muted-foreground truncate text-xs font-medium tracking-wider uppercase'>
          {props.title}
        </div>
      </div>
      {props.loading ? (
        <div className='mt-2 space-y-1.5'>
          <Skeleton className='h-6 w-24' />
          <Skeleton className='h-3 w-28' />
        </div>
      ) : (
        <>
          <div className='text-foreground mt-1.5 truncate font-mono text-xl font-semibold tabular-nums'>
            {props.value}
          </div>
          <div className='text-muted-foreground/60 mt-0.5 truncate text-xs'>
            {props.desc}
          </div>
        </>
      )}
    </div>
  )
}

function SummaryCards(props: { data: ProfitOverview; loading: boolean }) {
  const { t } = useTranslation()
  const summary = props.data.summary
  const profitTone = summary.profit_usd < 0 ? 'danger' : 'success'
  const items = [
    {
      title: t('Revenue'),
      value: formatUsd(summary.revenue_usd),
      desc: t('Consumption billing'),
      icon: CircleDollarSign,
    },
    {
      title: t('Estimated Cost'),
      value: formatUsd(summary.estimated_cost_usd),
      desc: t('After group ratio'),
      icon: Coins,
      tone: 'warning' as const,
    },
    {
      title: t('Estimated Profit'),
      value: formatUsd(summary.profit_usd),
      desc: t('Revenue minus estimated cost'),
      icon: TrendingUp,
      tone: profitTone as 'success' | 'danger',
    },
    {
      title: t('Profit Rate'),
      value: formatRate(summary.profit_rate),
      desc: t('Estimated margin'),
      icon: BarChart3,
      tone: profitTone as 'success' | 'danger',
    },
    {
      title: t('Top-up Amount'),
      value: formatAmount(summary.topup_amount),
      desc: t('Successful top-ups'),
      icon: CreditCard,
    },
    {
      title: t('Epay WeChat Pay'),
      value: formatAmount(summary.epay_wx_amount),
      desc: t('wxpay accumulated amount'),
      icon: WalletCards,
    },
    {
      title: t('Requests'),
      value: formatNumber(summary.request_count),
      desc: t('Successful calls'),
      icon: Zap,
    },
    {
      title: t('Failures'),
      value: formatNumber(summary.failed_count),
      desc: t('Failed relay logs'),
      icon: AlertTriangle,
      tone: summary.failed_count > 0 ? ('danger' as const) : undefined,
    },
  ]

  return (
    <div className='grid gap-2 sm:grid-cols-2 xl:grid-cols-4'>
      {items.map((item) => (
        <MetricCard
          key={item.title}
          title={item.title}
          value={item.value}
          desc={item.desc}
          icon={item.icon}
          tone={item.tone}
          loading={props.loading}
        />
      ))}
    </div>
  )
}

function FilterBar(props: {
  draft: ProfitFilterDraft
  activeFilters: ProfitQueryParams
  loading: boolean
  onDraftChange: (draft: ProfitFilterDraft) => void
  onApply: () => void
  onReset: () => void
  onRefresh: () => void
}) {
  const { t } = useTranslation()
  const {
    activeFilters,
    draft,
    loading,
    onApply,
    onDraftChange,
    onRefresh,
    onReset,
  } = props

  const activeBadges = useMemo(() => {
    const badges: string[] = []
    if (activeFilters.channel_id) {
      badges.push(`${t('Channel ID')}: ${activeFilters.channel_id}`)
    }
    if (activeFilters.channel_type) {
      badges.push(
        `${t('Provider')}: ${channelProviderLabel(
          activeFilters.channel_type,
          t
        )}`
      )
    }
    if (activeFilters.model_name) {
      badges.push(`${t('Model')}: ${activeFilters.model_name}`)
    }
    if (activeFilters.group) {
      badges.push(`${t('Group')}: ${activeFilters.group}`)
    }
    if (activeFilters.payment_provider) {
      badges.push(
        `${t('Payment Provider')}: ${paymentLabel(
          activeFilters.payment_provider,
          t
        )}`
      )
    }
    if (activeFilters.payment_method) {
      badges.push(
        `${t('Payment Method')}: ${paymentLabel(
          activeFilters.payment_method,
          t
        )}`
      )
    }
    return badges
  }, [activeFilters, t])

  const updateDraft = useCallback(
    (patch: Partial<ProfitFilterDraft>) => {
      onDraftChange({ ...draft, ...patch })
    },
    [draft, onDraftChange]
  )

  const handleInputChange =
    (field: keyof ProfitFilterDraft) =>
    (event: ChangeEvent<HTMLInputElement>) => {
      updateDraft({ [field]: event.target.value } as Partial<ProfitFilterDraft>)
    }

  const handleSelectChange =
    (field: 'channel_type' | 'payment_provider' | 'payment_method') =>
    (event: ChangeEvent<HTMLSelectElement>) => {
      updateDraft({ [field]: event.target.value } as Partial<ProfitFilterDraft>)
    }

  const handleTimeChange =
    (field: 'start_timestamp' | 'end_timestamp') =>
    (event: ChangeEvent<HTMLInputElement>) => {
      const value = parseTimestampFromInput(event.target.value)
      if (value > 0) updateDraft({ [field]: value })
    }

  return (
    <div className='rounded-lg border p-3 sm:p-4'>
      <div className='flex flex-col gap-3'>
        <div className='flex flex-wrap items-center justify-between gap-2'>
          <div className='flex items-center gap-2'>
            <SlidersHorizontal className='text-muted-foreground/70 size-4' />
            <span className='text-sm font-semibold'>{t('Filters')}</span>
            {activeBadges.length > 0 && (
              <Badge variant='secondary'>{t('Filters active')}</Badge>
            )}
          </div>
          <div className='flex min-w-0 flex-wrap items-center gap-1.5'>
            {activeBadges.map((badge) => (
              <Badge key={badge} variant='outline' className='max-w-64'>
                <span className='truncate'>{badge}</span>
              </Badge>
            ))}
          </div>
        </div>

        <div className='flex flex-wrap items-center gap-1.5'>
          {QUICK_RANGES.map((range) => (
            <Button
              key={range.days}
              type='button'
              size='xs'
              variant='outline'
              onClick={() => updateDraft(getRange(range.days))}
            >
              {t(range.label)}
            </Button>
          ))}
        </div>

        <div className='grid gap-2 md:grid-cols-2 xl:grid-cols-8'>
          <div className='text-muted-foreground text-xs font-semibold md:col-span-2 xl:col-span-8'>
            {t('Usage Filters')}
          </div>
          <div className='grid gap-1.5 xl:col-span-1'>
            <Label className='text-xs' htmlFor='profit-start-time'>
              {t('Start Time')}
            </Label>
            <Input
              id='profit-start-time'
              type='datetime-local'
              value={formatTimestampForInput(draft.start_timestamp)}
              onChange={handleTimeChange('start_timestamp')}
            />
          </div>
          <div className='grid gap-1.5 xl:col-span-1'>
            <Label className='text-xs' htmlFor='profit-end-time'>
              {t('End Time')}
            </Label>
            <Input
              id='profit-end-time'
              type='datetime-local'
              value={formatTimestampForInput(draft.end_timestamp)}
              onChange={handleTimeChange('end_timestamp')}
            />
          </div>
          <div className='grid gap-1.5'>
            <Label className='text-xs' htmlFor='profit-channel-provider'>
              {t('Provider')}
            </Label>
            <NativeSelect
              id='profit-channel-provider'
              className='w-full'
              value={draft.channel_type}
              onChange={handleSelectChange('channel_type')}
            >
              {CHANNEL_PROVIDERS.map((item) => (
                <NativeSelectOption key={item.value} value={item.value}>
                  {t(item.label)}
                </NativeSelectOption>
              ))}
            </NativeSelect>
          </div>
          <div className='grid gap-1.5'>
            <Label className='text-xs' htmlFor='profit-channel-id'>
              {t('Channel ID')}
            </Label>
            <Input
              id='profit-channel-id'
              inputMode='numeric'
              placeholder={t('All Channels')}
              value={draft.channel_id}
              onChange={handleInputChange('channel_id')}
            />
          </div>
          <div className='grid gap-1.5'>
            <Label className='text-xs' htmlFor='profit-model-name'>
              {t('Model')}
            </Label>
            <Input
              id='profit-model-name'
              placeholder={t('All Models')}
              value={draft.model_name}
              onChange={handleInputChange('model_name')}
            />
          </div>
          <div className='grid gap-1.5'>
            <Label className='text-xs' htmlFor='profit-group'>
              {t('Group')}
            </Label>
            <Input
              id='profit-group'
              placeholder={t('All Groups')}
              value={draft.group}
              onChange={handleInputChange('group')}
            />
          </div>
          <div className='text-muted-foreground text-xs font-semibold md:col-span-2 xl:col-span-8'>
            {t('Payment Filters')}
          </div>
          <div className='grid gap-1.5'>
            <Label className='text-xs' htmlFor='profit-provider'>
              {t('Payment Provider')}
            </Label>
            <NativeSelect
              id='profit-provider'
              className='w-full'
              value={draft.payment_provider}
              onChange={handleSelectChange('payment_provider')}
            >
              {PAYMENT_PROVIDERS.map((item) => (
                <NativeSelectOption key={item.value} value={item.value}>
                  {t(item.label)}
                </NativeSelectOption>
              ))}
            </NativeSelect>
          </div>
          <div className='grid gap-1.5'>
            <Label className='text-xs' htmlFor='profit-method'>
              {t('Payment Method')}
            </Label>
            <NativeSelect
              id='profit-method'
              className='w-full'
              value={draft.payment_method}
              onChange={handleSelectChange('payment_method')}
            >
              {PAYMENT_METHODS.map((item) => (
                <NativeSelectOption key={item.value} value={item.value}>
                  {t(item.label)}
                </NativeSelectOption>
              ))}
            </NativeSelect>
          </div>
          <div className='flex items-end gap-2 md:col-span-2 xl:col-span-8'>
            <Button type='button' variant='outline' onClick={onReset}>
              {t('Reset')}
            </Button>
            <Button type='button' onClick={onApply}>
              <Search data-icon='inline-start' />
              {t('Apply Filters')}
            </Button>
            <Button
              type='button'
              variant='outline'
              size='icon'
              aria-label={t('Refresh')}
              onClick={onRefresh}
              disabled={loading}
            >
              <RefreshCw
                className={cn('size-4', loading && 'animate-spin')}
              />
            </Button>
          </div>
        </div>
      </div>
    </div>
  )
}

function TrendRows(props: { trends: ProfitTrendItem[]; loading: boolean }) {
  const { t } = useTranslation()
  const items = useMemo(
    () => [...props.trends].sort((a, b) => a.created_at - b.created_at).slice(-18),
    [props.trends]
  )
  const maxValue = useMemo(() => {
    const values = items.flatMap((item) => [
      Math.abs(item.revenue_usd || 0),
      Math.abs(item.estimated_cost_usd || 0),
      Math.abs(item.profit_usd || 0),
    ])
    return Math.max(...values, 1)
  }, [items])

  if (props.loading) {
    return (
      <div className='space-y-2'>
        {Array.from({ length: 8 }).map((_, index) => (
          <Skeleton key={index} className='h-8 w-full' />
        ))}
      </div>
    )
  }

  if (items.length === 0) {
    return (
      <div className='text-muted-foreground flex h-40 items-center justify-center text-sm'>
        {t('No data available')}
      </div>
    )
  }

  return (
    <div className='space-y-2'>
      <div className='text-muted-foreground grid grid-cols-[5rem_1fr_5.5rem] gap-2 px-1 text-[11px] font-medium sm:grid-cols-[7rem_1fr_7rem]'>
        <span>{t('Time')}</span>
        <span>{t('Revenue / Cost / Profit')}</span>
        <span className='text-right'>{t('Profit')}</span>
      </div>
      {items.map((item) => {
        const profitTone =
          item.profit_usd < 0 ? 'bg-destructive' : 'bg-success'
        return (
          <div
            key={item.created_at}
            className='grid grid-cols-[5rem_1fr_5.5rem] items-center gap-2 rounded-md px-1 py-1.5 sm:grid-cols-[7rem_1fr_7rem]'
          >
            <span className='text-muted-foreground truncate text-xs'>
              {formatTimestampToDate(item.created_at).slice(5, 16)}
            </span>
            <div className='space-y-1'>
              <div className='bg-muted h-1.5 overflow-hidden rounded-full'>
                <span
                  className='block h-full rounded-full bg-primary'
                  style={{
                    width: `${Math.max(3, (Math.abs(item.revenue_usd) / maxValue) * 100)}%`,
                  }}
                />
              </div>
              <div className='bg-muted h-1.5 overflow-hidden rounded-full'>
                <span
                  className='block h-full rounded-full bg-warning'
                  style={{
                    width: `${Math.max(3, (Math.abs(item.estimated_cost_usd) / maxValue) * 100)}%`,
                  }}
                />
              </div>
              <div className='bg-muted h-1.5 overflow-hidden rounded-full'>
                <span
                  className={cn('block h-full rounded-full', profitTone)}
                  style={{
                    width: `${Math.max(3, (Math.abs(item.profit_usd) / maxValue) * 100)}%`,
                  }}
                />
              </div>
            </div>
            <span
              className={cn(
                'truncate text-right font-mono text-xs font-semibold',
                item.profit_usd < 0 ? 'text-destructive' : 'text-success'
              )}
              title={formatUsd(item.profit_usd)}
            >
              {formatUsd(item.profit_usd)}
            </span>
          </div>
        )
      })}
    </div>
  )
}

function ChannelTable(props: { channels: ProfitChannelItem[]; loading: boolean }) {
  const { t } = useTranslation()
  const channels = useMemo(
    () =>
      [...props.channels]
        .sort((a, b) => b.profit_usd - a.profit_usd)
        .slice(0, 10),
    [props.channels]
  )

  if (props.loading) {
    return <Skeleton className='h-64 w-full' />
  }

  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>{t('Channel')}</TableHead>
          <TableHead>{t('Provider')}</TableHead>
          <TableHead className='text-right'>{t('Cost Ratio')}</TableHead>
          <TableHead className='text-right'>{t('Revenue')}</TableHead>
          <TableHead className='text-right'>{t('Estimated Cost')}</TableHead>
          <TableHead className='text-right'>{t('Profit')}</TableHead>
          <TableHead className='text-right'>{t('Profit Rate')}</TableHead>
          <TableHead className='text-right'>{t('Requests')}</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {channels.length === 0 ? (
          <TableRow>
            <TableCell colSpan={8} className='text-muted-foreground h-32 text-center'>
              {t('No data available')}
            </TableCell>
          </TableRow>
        ) : (
          channels.map((channel) => (
            <TableRow key={channel.channel_id}>
              <TableCell className='max-w-56'>
                <div className='truncate font-medium' title={channel.channel_name}>
                  {channel.channel_name}
                </div>
                <div className='text-muted-foreground text-xs'>
                  #{channel.channel_id}
                </div>
              </TableCell>
              <TableCell>{channel.channel_type_name || '-'}</TableCell>
              <TableCell className='text-right font-mono'>
                {formatCostRatio(channel.cost_ratio)}
              </TableCell>
              <TableCell className='text-right font-mono'>
                {formatUsd(channel.revenue_usd)}
              </TableCell>
              <TableCell className='text-right font-mono'>
                {formatUsd(channel.estimated_cost_usd)}
              </TableCell>
              <TableCell
                className={cn(
                  'text-right font-mono font-semibold',
                  channel.profit_usd < 0 ? 'text-destructive' : 'text-success'
                )}
              >
                {formatUsd(channel.profit_usd)}
              </TableCell>
              <TableCell className='text-right font-mono'>
                {formatRate(channel.profit_rate)}
              </TableCell>
              <TableCell className='text-right font-mono'>
                {formatNumber(channel.request_count)}
              </TableCell>
            </TableRow>
          ))
        )}
      </TableBody>
    </Table>
  )
}

function ModelTable(props: { models: ProfitModelItem[]; loading: boolean }) {
  const { t } = useTranslation()
  const models = useMemo(
    () =>
      [...props.models]
        .sort((a, b) => b.revenue_usd - a.revenue_usd)
        .slice(0, 10),
    [props.models]
  )

  if (props.loading) {
    return <Skeleton className='h-64 w-full' />
  }

  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>{t('Model')}</TableHead>
          <TableHead className='text-right'>{t('Revenue')}</TableHead>
          <TableHead className='text-right'>{t('Estimated Cost')}</TableHead>
          <TableHead className='text-right'>{t('Profit')}</TableHead>
          <TableHead className='text-right'>{t('Profit Rate')}</TableHead>
          <TableHead className='text-right'>{t('Tokens')}</TableHead>
          <TableHead className='text-right'>{t('Requests')}</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {models.length === 0 ? (
          <TableRow>
            <TableCell colSpan={7} className='text-muted-foreground h-32 text-center'>
              {t('No data available')}
            </TableCell>
          </TableRow>
        ) : (
          models.map((model) => (
            <TableRow key={model.model_name}>
              <TableCell className='max-w-64'>
                <span className='truncate font-mono' title={model.model_name}>
                  {model.model_name}
                </span>
              </TableCell>
              <TableCell className='text-right font-mono'>
                {formatUsd(model.revenue_usd)}
              </TableCell>
              <TableCell className='text-right font-mono'>
                {formatUsd(model.estimated_cost_usd)}
              </TableCell>
              <TableCell
                className={cn(
                  'text-right font-mono font-semibold',
                  model.profit_usd < 0 ? 'text-destructive' : 'text-success'
                )}
              >
                {formatUsd(model.profit_usd)}
              </TableCell>
              <TableCell className='text-right font-mono'>
                {formatRate(model.profit_rate)}
              </TableCell>
              <TableCell className='text-right font-mono'>
                {formatTokens(model.prompt_tokens, model.completion_tokens)}
              </TableCell>
              <TableCell className='text-right font-mono'>
                {formatNumber(model.request_count)}
              </TableCell>
            </TableRow>
          ))
        )}
      </TableBody>
    </Table>
  )
}

function TopUpTable(props: { topups: ProfitTopUpItem[]; loading: boolean }) {
  const { t } = useTranslation()
  const topups = useMemo(
    () => [...props.topups].sort((a, b) => b.money - a.money),
    [props.topups]
  )

  if (props.loading) {
    return <Skeleton className='h-48 w-full' />
  }

  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>{t('Payment Provider')}</TableHead>
          <TableHead>{t('Payment Method')}</TableHead>
          <TableHead className='text-right'>{t('Top-up Amount')}</TableHead>
          <TableHead className='text-right'>{t('Top-up Count')}</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {topups.length === 0 ? (
          <TableRow>
            <TableCell colSpan={4} className='text-muted-foreground h-24 text-center'>
              {t('No data available')}
            </TableCell>
          </TableRow>
        ) : (
          topups.map((topup) => (
            <TableRow
              key={`${topup.payment_provider}:${topup.payment_method || '-'}`}
            >
              <TableCell>
                {paymentLabel(topup.payment_provider, t)}
              </TableCell>
              <TableCell>{paymentLabel(topup.payment_method, t)}</TableCell>
              <TableCell className='text-right font-mono'>
                {formatAmount(topup.money)}
              </TableCell>
              <TableCell className='text-right font-mono'>
                {formatNumber(topup.count)}
              </TableCell>
            </TableRow>
          ))
        )}
      </TableBody>
    </Table>
  )
}

export function ProfitVisualization() {
  const { t } = useTranslation()
  const [draft, setDraft] = useState<ProfitFilterDraft>(() =>
    createDefaultDraft()
  )
  const [filters, setFilters] = useState<ProfitQueryParams>(() =>
    toQueryParams(createDefaultDraft())
  )

  const profitQuery = useQuery({
    queryKey: ['profit-overview', filters],
    queryFn: () => getProfitOverview(filters),
    staleTime: 30 * 1000,
  })

  const overview =
    profitQuery.data?.success && profitQuery.data.data
      ? profitQuery.data.data
      : EMPTY_OVERVIEW
  const loading = profitQuery.isLoading
  const hasError = profitQuery.isError || profitQuery.data?.success === false
  const displayStartTimestamp =
    overview.summary.start_timestamp || filters.start_timestamp
  const displayEndTimestamp =
    overview.summary.end_timestamp || filters.end_timestamp

  const handleApply = useCallback(() => {
    setFilters(toQueryParams(draft))
  }, [draft])

  const handleReset = useCallback(() => {
    const next = createDefaultDraft()
    setDraft(next)
    setFilters(toQueryParams(next))
  }, [])

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>
        {t('Profit Visualization')}
      </SectionPageLayout.Title>
      <SectionPageLayout.Content>
        <div className='space-y-3 sm:space-y-4'>
          <FadeIn>
            <FilterBar
              draft={draft}
              activeFilters={filters}
              loading={profitQuery.isFetching}
              onDraftChange={setDraft}
              onApply={handleApply}
              onReset={handleReset}
              onRefresh={() => profitQuery.refetch()}
            />
          </FadeIn>

          {hasError && (
            <div className='border-destructive/40 bg-destructive/5 text-destructive rounded-lg border px-3 py-2 text-sm'>
              {profitQuery.data?.message || t('Failed to load profit data')}
            </div>
          )}

          {overview.summary.truncated && (
            <div className='border-warning/40 bg-warning/5 text-warning rounded-lg border px-3 py-2 text-sm'>
              {t('Profit data is limited to the latest {{count}} log rows.', {
                count: overview.summary.truncated_limit,
              })}
            </div>
          )}

          <FadeIn delay={0.05}>
            <SummaryCards data={overview} loading={loading} />
          </FadeIn>

          <div className='grid gap-3 xl:grid-cols-[minmax(0,0.9fr)_minmax(0,1.1fr)]'>
            <FadeIn delay={0.1}>
              <ProfitPanel
                title={t('Revenue Trend')}
                description={t('Hourly revenue, cost and profit')}
              >
                <TrendRows trends={overview.trends} loading={loading} />
              </ProfitPanel>
            </FadeIn>

            <FadeIn delay={0.15}>
              <ProfitPanel
                title={t('Channel Profit')}
                description={t('Top channels by estimated profit')}
              >
                <ChannelTable
                  channels={overview.channels}
                  loading={loading}
                />
              </ProfitPanel>
            </FadeIn>
          </div>

          <div className='grid gap-3 xl:grid-cols-[minmax(0,1.1fr)_minmax(0,0.9fr)]'>
            <FadeIn delay={0.2}>
              <ProfitPanel
                title={t('Model Profit')}
                description={t('Top models by consumption revenue')}
              >
                <ModelTable models={overview.models} loading={loading} />
              </ProfitPanel>
            </FadeIn>

            <FadeIn delay={0.25}>
              <ProfitPanel
                title={t('Payment Breakdown')}
                description={t('Successful top-ups by provider and method')}
              >
                <TopUpTable topups={overview.topups} loading={loading} />
              </ProfitPanel>
            </FadeIn>
          </div>

          <FadeIn delay={0.3}>
            <div className='text-muted-foreground flex flex-wrap items-center gap-x-4 gap-y-1 text-xs'>
              <span>
                {t('Range')}: {formatTimestampToDate(displayStartTimestamp)} -{' '}
                {formatTimestampToDate(displayEndTimestamp)}
              </span>
              <span>
                {t('Average Top-up')}: {formatAmount(overview.summary.avg_topup_amount)}
              </span>
              <span>
                {t('Top-up Count')}: {formatNumber(overview.summary.topup_count)}
              </span>
              <span>
                {t('Estimated only')}
              </span>
            </div>
          </FadeIn>
        </div>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
