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
export interface ProfitQueryParams {
  start_timestamp: number
  end_timestamp: number
  channel_id?: number
  channel_type?: number
  model_name?: string
  group?: string
  payment_provider?: string
  payment_method?: string
}

export interface ProfitSummary {
  start_timestamp: number
  end_timestamp: number
  topup_amount: number
  epay_wx_amount: number
  revenue_usd: number
  estimated_cost_usd: number
  profit_usd: number
  cost_ratio: number
  profit_rate: number
  request_count: number
  failed_count: number
  topup_count: number
  avg_topup_amount: number
  truncated: boolean
  truncated_limit: number
}

export interface ProfitTrendItem {
  created_at: number
  topup_amount: number
  revenue_usd: number
  estimated_cost_usd: number
  profit_usd: number
  request_count: number
  failed_count: number
}

export interface ProfitChannelItem {
  channel_id: number
  channel_name: string
  channel_type: number
  channel_type_name: string
  cost_ratio: number
  revenue_usd: number
  estimated_cost_usd: number
  profit_usd: number
  profit_rate: number
  request_count: number
  failed_count: number
  prompt_tokens: number
  completion_tokens: number
}

export interface ProfitModelItem {
  model_name: string
  cost_ratio: number
  revenue_usd: number
  estimated_cost_usd: number
  profit_usd: number
  profit_rate: number
  request_count: number
  failed_count: number
  prompt_tokens: number
  completion_tokens: number
}

export interface ProfitTopUpItem {
  payment_provider: string
  payment_method: string
  money: number
  count: number
}

export interface ProfitOverview {
  summary: ProfitSummary
  trends: ProfitTrendItem[]
  channels: ProfitChannelItem[]
  models: ProfitModelItem[]
  topups: ProfitTopUpItem[]
}

export interface ProfitCostRatioConfig {
  default_ratio?: number | null
  provider_ratios: Record<string, number>
  channel_ratios: Record<string, number>
  model_ratios: Record<string, number>
  provider_model_ratios: Record<string, number>
  channel_model_ratios: Record<string, number>
}

export interface ProfitPreviewRequest extends ProfitQueryParams {
  cost_ratio_config: ProfitCostRatioConfig
}

export interface ProfitOverviewResponse {
  success: boolean
  message?: string
  data?: ProfitOverview
}

export interface ProfitCostRatioConfigResponse {
  success: boolean
  message?: string
  data?: ProfitCostRatioConfig
}
