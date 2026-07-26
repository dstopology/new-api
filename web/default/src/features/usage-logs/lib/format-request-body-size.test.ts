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
import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { formatRequestBodySize } from './format'

describe('formatRequestBodySize', () => {
  it('omits missing and invalid sizes', () => {
    assert.equal(formatRequestBodySize(undefined), null)
    assert.equal(formatRequestBodySize(null), null)
    assert.equal(formatRequestBodySize(0), null)
    assert.equal(formatRequestBodySize(Number.NaN), null)
  })

  it('keeps normal request body labels compact', () => {
    assert.equal(formatRequestBodySize(512), '0.5KB')
    assert.equal(formatRequestBodySize(1536), '1.5KB')
    assert.equal(formatRequestBodySize(842 * 1024), '842KB')
    assert.equal(formatRequestBodySize(1.6 * 1024 ** 2), '1.6MB')
    assert.equal(formatRequestBodySize(25 * 1024 ** 2), '25MB')
  })

  it('uses larger units for extreme Content-Length values', () => {
    assert.equal(formatRequestBodySize(2.25 * 1024 ** 3), '2.3GB')
    assert.equal(formatRequestBodySize(128 * 1024 ** 4), '128TB')
  })
})
