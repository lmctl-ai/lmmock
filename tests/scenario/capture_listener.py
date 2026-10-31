"""Linux capture-job readiness for lminspect v0.1.0, which logs the requested port.
Read the owned child's listening socket instead of reserving and releasing a port.
This is test harness lifecycle, not mock timing or response policy.
"""
import pathlib

def listener_address(pid, proc=pathlib.Path('/proc')):
 folder=proc/str(pid)
 try:
  sockets={entry.readlink().name for entry in (folder/'fd').iterdir()}
  for line in (folder/'net/tcp').read_text().splitlines()[1:]:
   fields=line.split()
   if fields[3]=='0A' and 'socket:['+fields[9]+']' in sockets:
    host,port=fields[1].split(':')
    if host=='0100007F' and int(port,16):return '127.0.0.1',int(port,16)
 except FileNotFoundError:pass # Descriptor/process may close during inspection.
 return None

def wait_listener(child):
 if not pathlib.Path('/proc/self/fd').is_dir():
  raise RuntimeError('lminspect v0.1.0 capture readiness requires Linux procfs')
 while child.poll() is None:
  address=listener_address(child.pid)
  if address:return address
 raise RuntimeError('capture proxy exited before opening its owned listener')
