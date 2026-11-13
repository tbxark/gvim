-- gvim.textsafe: sanitizer for untrusted display text (PR titles, network text, file names).
--
-- strip(text) turns Cc controls into spaces and removes Cf format marks (bidi
-- overrides/isolates, ZWSP/ZWJ/ZWNJ, soft hyphen, BOM, tags, interlinear annotations, ...), so
-- hostile text cannot visually reorder or hide a row or inject a new line. Invalid UTF-8
-- bytes become spaces rather than reaching the terminal. The Cf table is the assigned set as
-- of Unicode 15.1. Callers bound the input length themselves and collapse whitespace.
local M = {}

local CF_RANGES = {
  { 0x00AD, 0x00AD }, { 0x0600, 0x0605 }, { 0x061C, 0x061C }, { 0x06DD, 0x06DD },
  { 0x070F, 0x070F }, { 0x0890, 0x0891 }, { 0x08E2, 0x08E2 }, { 0x180E, 0x180E },
  { 0x200B, 0x200F }, { 0x202A, 0x202E }, { 0x2060, 0x2064 }, { 0x2066, 0x206F },
  { 0xFEFF, 0xFEFF }, { 0xFFF9, 0xFFFB }, { 0x110BD, 0x110BD }, { 0x110CD, 0x110CD },
  { 0x13430, 0x1343F }, { 0x1BCA0, 0x1BCA3 }, { 0x1D173, 0x1D17A },
  { 0xE0001, 0xE0001 }, { 0xE0020, 0xE007F },
}

local function is_cf(cp)
  for _, range in ipairs(CF_RANGES) do
    if cp >= range[1] and cp <= range[2] then
      return true
    end
  end
  return false
end

local function utf8_codepoint(text, i)
  local b1 = text:byte(i)
  if not b1 then
    return nil, nil
  end
  if b1 < 0x80 then
    return b1, 1
  end
  if b1 >= 0xC2 and b1 <= 0xDF then
    local b2 = text:byte(i + 1)
    if b2 and b2 >= 0x80 and b2 <= 0xBF then
      return (b1 - 0xC0) * 0x40 + (b2 - 0x80), 2
    end
  elseif b1 >= 0xE0 and b1 <= 0xEF then
    local b2, b3 = text:byte(i + 1), text:byte(i + 2)
    if b2 and b3 and b2 >= 0x80 and b2 <= 0xBF and b3 >= 0x80 and b3 <= 0xBF then
      local cp = (b1 - 0xE0) * 0x1000 + (b2 - 0x80) * 0x40 + (b3 - 0x80)
      if cp >= 0x800 and not (cp >= 0xD800 and cp <= 0xDFFF) then
        return cp, 3
      end
    end
  elseif b1 >= 0xF0 and b1 <= 0xF4 then
    local b2, b3, b4 = text:byte(i + 1), text:byte(i + 2), text:byte(i + 3)
    if b2 and b3 and b4 and b2 >= 0x80 and b2 <= 0xBF and b3 >= 0x80 and b3 <= 0xBF and b4 >= 0x80 and b4 <= 0xBF then
      local cp = (b1 - 0xF0) * 0x40000 + (b2 - 0x80) * 0x1000 + (b3 - 0x80) * 0x40 + (b4 - 0x80)
      if cp >= 0x10000 and cp <= 0x10FFFF then
        return cp, 4
      end
    end
  end
  return nil, 1
end

local function strip(text)
  local out = {}
  local i = 1
  while i <= #text do
    local cp, width = utf8_codepoint(text, i)
    if not cp or width == nil then
      out[#out + 1] = " "
      i = i + 1
    elseif cp <= 0x1F or cp == 0x7F or (cp >= 0x80 and cp <= 0x9F) then
      out[#out + 1] = " "
      i = i + width
    elseif is_cf(cp) then
      -- Cf marks are invisible/format-only: remove them, keeping surrounding text.
      i = i + width
    else
      out[#out + 1] = text:sub(i, i + width - 1)
      i = i + width
    end
  end
  return table.concat(out)
end

M.strip = strip

return M
