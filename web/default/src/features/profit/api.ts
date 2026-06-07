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
import { api } from '@/lib/api'
import type {
  ProfitCostRatioConfig,
  ProfitCostRatioConfigResponse,
  ProfitOverviewResponse,
  ProfitPreviewRequest,
  ProfitQueryParams,
} from './types'

export async function getProfitOverview(params: ProfitQueryParams) {
  const res = await api.get<ProfitOverviewResponse>('/api/profit/summary', {
    params,
  })
  return res.data
}

export async function previewProfitOverview(request: ProfitPreviewRequest) {
  const res = await api.post<ProfitOverviewResponse>(
    '/api/profit/summary/preview',
    request
  )
  return res.data
}

export async function getProfitCostRatioConfig() {
  const res = await api.get<ProfitCostRatioConfigResponse>(
    '/api/profit/cost-ratio-config'
  )
  return res.data
}

export async function updateProfitCostRatioConfig(
  config: ProfitCostRatioConfig
) {
  const res = await api.put<ProfitCostRatioConfigResponse>(
    '/api/profit/cost-ratio-config',
    config
  )
  return res.data
}
