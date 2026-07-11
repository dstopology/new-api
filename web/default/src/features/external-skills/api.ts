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
  ApiResponse,
  ExternalSkill,
  ExternalSkillInput,
  ExternalSkillsPage,
} from './types'

export async function listExternalSkills(params: {
  page: number
  pageSize: number
  keyword: string
}): Promise<ApiResponse<ExternalSkillsPage>> {
  const response = await api.get('/api/external-skills', {
    params: {
      p: params.page,
      page_size: params.pageSize,
      keyword: params.keyword || undefined,
    },
  })
  return response.data
}

export async function getExternalSkill(
  id: number
): Promise<ApiResponse<ExternalSkill>> {
  const response = await api.get(`/api/external-skills/${id}`)
  return response.data
}

export async function createExternalSkill(
  input: ExternalSkillInput
): Promise<ApiResponse<ExternalSkill>> {
  const response = await api.post('/api/external-skills', input)
  return response.data
}

export async function updateExternalSkill(
  id: number,
  input: ExternalSkillInput
): Promise<ApiResponse<ExternalSkill>> {
  const response = await api.put(`/api/external-skills/${id}`, input)
  return response.data
}

export async function deleteExternalSkill(id: number): Promise<ApiResponse> {
  const response = await api.delete(`/api/external-skills/${id}`)
  return response.data
}
