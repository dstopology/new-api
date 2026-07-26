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
import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import { channelSchema } from '../types'
import {
  CHANNEL_FORM_DEFAULT_VALUES,
  transformChannelToFormDefaults,
  transformFormDataToCreatePayload,
} from './channel-form'

describe('Responses stream auto-resume channel setting', () => {
  it('serializes the enabled setting into channel other settings', () => {
    const result = transformFormDataToCreatePayload({
      ...CHANNEL_FORM_DEFAULT_VALUES,
      name: 'OpenAI',
      key: 'sk-test',
      models: 'gpt-5',
      enable_responses_stream_resume: true,
    })

    const settings = JSON.parse(String(result.channel.settings))
    assert.equal(settings.enable_responses_stream_resume, true)
  })

  it('restores the enabled setting when editing a channel', () => {
    const channel = channelSchema.parse({
      id: 1,
      type: 1,
      key: '',
      status: 1,
      name: 'OpenAI',
      created_time: 0,
      test_time: 0,
      response_time: 0,
      balance_updated_time: 0,
      settings: JSON.stringify({
        enable_responses_stream_resume: true,
      }),
    })

    const defaults = transformChannelToFormDefaults(channel)
    assert.equal(defaults.enable_responses_stream_resume, true)
  })
})
