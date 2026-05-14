local function inner()
  local function trace()
    local out = ''
    for i=0,10 do
      local s = debug.info(i, 's')
      local l = debug.info(i, 'l')
      if not s then break end
      out = out .. tostring(i) .. ':' .. tostring(s) .. ':' .. tostring(l) .. '|'
    end
    return out
  end
  return trace()
end
print(inner())
