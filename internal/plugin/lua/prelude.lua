-- gvim plugin prelude. Compiled once per process, instantiated once per plugin state.
--
-- The Go side passes everything the prelude needs as arguments, so plugin code can never
-- reach (or tamper with) the yield tag, the raw yield/resume functions or the originals:
--   tag         unforgeable userdata that marks a yield as a call into the host
--   raw_yield   yields its arguments unchanged (only the prelude holds it)
--   raw_resume  resumes a coroutine: refuses roots and the main thread, refreshes the
--               context, bounds the nesting depth
--   await       Go function that yields (tag, kind, ...) to the host
--   pub_yield   what user code may yield with: refuses roots and the main thread
--   go          table of Go helpers: find, done (require), can_yield
--
-- Yield protocol. An async API function is created by M.mk(kind) and looks like a blocking
-- call. It yields (tag, kind, args...) up to the root task, which the host resumes with
-- (ok, v1, v2, ...) (ok false: a programmer error, raised in the caller's frame). Nested
-- coroutines (coroutine.wrap, pcall, generators) forward tagged yields to the root; untagged
-- yields belong to the user and pass through. No await may be in tail position of a Go
-- function, which is why every API wrapper below stores its results in locals.
local tag, raw_yield, raw_resume, await, create, status, unpack, select, type, pub_yield, error, go = ...

local function pack(...) return { n = select("#", ...), ... } end

-- drive resumes co and, while it awaits, re-yields the same tagged values from the calling
-- thread (which is itself a coroutine whose own driver forwards again, up to the root).
local function drive(co, ...)
  local r = pack(raw_resume(co, ...))
  while r[1] and r[2] == tag and status(co) == "suspended" do
    r = pack(raw_resume(co, raw_yield(unpack(r, 2, r.n))))
  end
  return unpack(r, 1, r.n)
end

local M = {}

function M.resume(co, ...) return drive(co, ...) end

-- entry makes f usable as a coroutine or task body. A coroutine whose body is a Go function
-- loses its results (and fails with a nil error), so a Go function is wrapped in a Lua closure.
local function entry(f)
  if type(f) == "function" and go.is_go(f) then
    local g = f
    return function(...)
      local r = pack(g(...))
      return unpack(r, 1, r.n)
    end
  end
  return f
end

-- call is the body of a root task whose function is a Go function: the host passes the Go
-- function as the first argument.
function M.call(g, ...)
  local r = pack(g(...))
  return unpack(r, 1, r.n)
end

function M.create(f) return create(entry(f)) end

function M.wrap(f)
  local co = create(entry(f))
  return function(...)
    local r = pack(drive(co, ...))
    if not r[1] then error(r[2], 0) end
    return unpack(r, 2, r.n)
  end
end

-- yield is a Lua function so that `return coroutine.yield(x)` is never a tail call of a
-- Go function (gopher-lua crashes on that).
function M.yield(...)
  local r = pack(pub_yield(...))
  return unpack(r, 1, r.n)
end

-- pcall runs f in its own coroutine so that awaits inside f reach the root task. f is always
-- wrapped in a Lua closure: a non-function so that errors and __call behave like stock pcall,
-- and a Go function because a coroutine whose body is a Go function loses its results.
function M.pcall(f, ...)
  local g = f
  f = function(...)
    local r = pack(g(...))
    return unpack(r, 1, r.n)
  end
  local co = create(f)
  local r = pack(raw_resume(co, ...))
  while true do
    if not r[1] then return false, r[2] end
    if status(co) == "dead" then return unpack(r, 1, r.n) end
    if r[2] == tag then
      -- an await: forward it to the root task
      r = pack(raw_resume(co, raw_yield(unpack(r, 2, r.n))))
    else
      -- a yield of the user's own generator passes through pcall; a root task cannot yield
      if not go.can_yield() then return false, "attempt to yield from outside a coroutine" end
      r = pack(raw_resume(co, pub_yield(unpack(r, 2, r.n))))
    end
  end
end

function M.xpcall(f, handler, ...)
  local r = pack(M.pcall(f, ...))
  if r[1] then return unpack(r, 1, r.n) end
  return false, handler(r[2])
end

-- mk(kind) makes an async function that returns one value, or (nil, err) on failure.
function M.mk(kind)
  return function(...)
    local ok, a, b = await(kind, ...)
    if not ok then error(a, 0) end
    if b == nil then return a end
    return a, b
  end
end

-- mkv(kind) is mk for the few calls that return any number of values.
function M.mkv(kind)
  return function(...)
    local r = pack(await(kind, ...))
    if not r[1] then error(r[2], 0) end
    return unpack(r, 2, r.n)
  end
end

-- require: Go resolves the name and hands back the loader (or the cached value); the
-- loader runs here, in Lua, so a module may await while it loads.
function M.require(name)
  local loader, cached = go.find(name)
  if not loader then return cached end
  local r = pack(M.pcall(loader, name))
  local value = go.done(name, r[1], r[2])
  if not r[1] then error(r[2], 0) end
  return value
end

return M
