package amqp

import "strconv"

func (p *peer) dispatch(id uint16, c *channel, class, method uint16, d *decoder) *fault {
	switch {
	case class == 50 && method == 10: // queue.declare
		d.u16()
		name := d.short()
		flags := d.u8()
		arguments := d.long()
		if !d.done() || flags&0xe0 != 0 {
			return fail(502, "SYNTAX_ERROR")
		}
		if flags&12 != 0 || len(arguments) != 0 {
			return fail(540, "NOT_IMPLEMENTED - queue options")
		}
		s := p.server
		s.mu.Lock()
		if name == "" {
			s.nextQueue++
			name = "amq.gen-lmmock-" + strconv.FormatUint(s.nextQueue, 10)
		}
		key := queueKey{p.vhost, name}
		q := s.queues[key]
		if q == nil {
			if flags&1 != 0 {
				s.mu.Unlock()
				return fail(404, "NOT_FOUND - queue")
			}
			if len(s.queues) >= 1024 {
				s.mu.Unlock()
				return fail(506, "RESOURCE_ERROR - queue limit")
			}
			q = &queue{durable: flags&2 != 0}
			s.queues[key] = q
		} else if q.durable != (flags&2 != 0) && flags&1 == 0 {
			s.mu.Unlock()
			return fail(406, "PRECONDITION_FAILED - queue options")
		}
		count := len(q.messages)
		s.mu.Unlock()
		c.lastQueue = name
		if flags&16 == 0 {
			var b encoder
			b.short(name)
			b.u32(uint32(count))
			b.u32(0)
			if p.method(id, 50, 11, b) != nil {
				return fail(541, "INTERNAL_ERROR - write failed")
			}
		}
	case class == 85 && method == 10: // confirm.select
		flags := d.u8()
		if !d.done() || flags&0xfe != 0 {
			return fail(502, "SYNTAX_ERROR")
		}
		c.confirm = true
		if flags&1 == 0 {
			if p.method(id, 85, 11, nil) != nil {
				return fail(541, "INTERNAL_ERROR - write failed")
			}
		}
	case class == 60 && method == 40: // basic.publish (content follows)
		d.u16()
		exchange := d.short()
		name := d.short()
		flags := d.u8()
		if !d.done() || flags&0xfc != 0 {
			return fail(502, "SYNTAX_ERROR")
		}
		if exchange != "" || flags&2 != 0 {
			return fail(540, "NOT_IMPLEMENTED - exchange or immediate publish")
		}
		key := queueKey{p.vhost, name}
		p.server.mu.Lock()
		exists := p.server.queues[key] != nil
		p.server.mu.Unlock()
		if !exists {
			return fail(404, "NOT_FOUND - routing queue")
		}
		c.pending = &pending{key: key}
	case class == 60 && method == 70: // basic.get, deliberately not basic.consume
		d.u16()
		name := d.short()
		flags := d.u8()
		if !d.done() || flags&0xfe != 0 {
			return fail(502, "SYNTAX_ERROR")
		}
		if flags&1 == 0 {
			return fail(540, "NOT_IMPLEMENTED - manual acknowledgments")
		}
		if name == "" {
			name = c.lastQueue
		}
		s := p.server
		s.mu.Lock()
		q := s.queues[queueKey{p.vhost, name}]
		if q == nil {
			s.mu.Unlock()
			return fail(404, "NOT_FOUND - queue")
		}
		if len(q.messages) == 0 {
			s.mu.Unlock()
			var b encoder
			b.short("")
			if p.method(id, 60, 72, b) != nil {
				return fail(541, "INTERNAL_ERROR - write failed")
			}
			return nil
		}
		m := q.messages[0]
		q.messages[0] = message{}
		q.messages = q.messages[1:]
		remaining := len(q.messages)
		s.messages--
		s.bytes -= len(m.body) + len(m.properties)
		s.mu.Unlock()
		c.delivered++
		var b encoder
		b.u64(c.delivered)
		b.u8(0)
		b.short("")
		b.short(name)
		b.u32(uint32(remaining))
		if p.method(id, 60, 71, b) != nil {
			return fail(541, "INTERNAL_ERROR - write failed")
		}
		var h encoder
		h.u16(60)
		h.u16(0)
		h.u64(uint64(len(m.body)))
		h = append(h, m.properties...)
		if p.send(frame{2, id, h}) != nil {
			return fail(541, "INTERNAL_ERROR - write failed")
		}
		for body := m.body; len(body) > 0; {
			n := min(len(body), int(p.frameMax)-8)
			if p.send(frame{3, id, body[:n]}) != nil {
				return fail(541, "INTERNAL_ERROR - write failed")
			}
			body = body[n:]
		}
	default:
		return fail(540, "NOT_IMPLEMENTED - method")
	}
	return nil
}
func (p *peer) content(f frame, c *channel) *fault {
	pending := c.pending
	switch f.kind {
	case 2:
		if pending.header {
			return fail(505, "UNEXPECTED_FRAME")
		}
		d := decoder{p: f.payload}
		class, weight, size := d.u16(), d.u16(), d.u64()
		if d.bad || class != 60 || weight != 0 || !validProperties(d.p) {
			return fail(502, "SYNTAX_ERROR - content header")
		}
		if size > bodyLimit {
			return fail(311, "CONTENT_TOO_LARGE")
		}
		cost := int(size) + len(d.p)
		s := p.server
		s.mu.Lock()
		if s.bytes+s.inflight+cost > 32<<20 {
			s.mu.Unlock()
			return fail(506, "RESOURCE_ERROR - content limit")
		}
		s.inflight += cost
		s.mu.Unlock()
		pending.reserved = cost
		pending.size = size
		pending.header = true
		pending.properties = append([]byte(nil), d.p...)
		pending.body = make([]byte, 0, int(size))
	case 3:
		if !pending.header || len(f.payload) == 0 || uint64(len(pending.body)+len(f.payload)) > pending.size {
			return fail(505, "UNEXPECTED_FRAME - content body")
		}
		pending.body = append(pending.body, f.payload...)
	default:
		return fail(505, "UNEXPECTED_FRAME")
	}
	if !pending.header || uint64(len(pending.body)) != pending.size {
		return nil
	}
	s := p.server
	s.mu.Lock()
	q := s.queues[pending.key]
	if q == nil {
		s.mu.Unlock()
		return fail(404, "NOT_FOUND - queue")
	}
	if s.messages >= 10000 {
		s.mu.Unlock()
		return fail(506, "RESOURCE_ERROR - message limit")
	}
	q.messages = append(q.messages, message{body: pending.body, properties: pending.properties})
	s.bytes += pending.reserved
	s.inflight -= pending.reserved
	pending.reserved = 0
	s.messages++
	s.mu.Unlock()
	// A confirmation cannot be emitted until the entire message is in the FIFO.
	c.pending = nil
	if c.confirm {
		c.published++
		var b encoder
		b.u64(c.published)
		b.u8(0)
		if p.method(f.channel, 60, 80, b) != nil {
			return fail(541, "INTERNAL_ERROR - write failed")
		}
	}
	return nil
}
func (p *peer) drop(c *channel) {
	if c.pending != nil {
		p.server.mu.Lock()
		p.server.inflight -= c.pending.reserved
		p.server.mu.Unlock()
		c.pending = nil
	}
}
