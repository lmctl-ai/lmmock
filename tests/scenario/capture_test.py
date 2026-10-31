import os,pathlib,runpy,subprocess,sys,tempfile,unittest
from unittest.mock import patch
root=pathlib.Path(__file__).resolve().parents[2]
class LaunchObserved(Exception): pass
class CaptureListenerTests(unittest.TestCase):
 def test_capture_child_allocates_its_own_listener(self):
  def observe(argv,**kwargs):
   self.assertEqual(argv[argv.index('--listen')+1],'127.0.0.1:0','capture must let the child own ephemeral port allocation')
   raise LaunchObserved()
  with tempfile.TemporaryDirectory() as temp,patch.dict(os.environ,{'LMINSPECT_BIN':'owned-inspect','LMMOCK_URL':'http://127.0.0.1:1'}),patch.object(sys,'argv',['capture.py',temp]),patch('subprocess.Popen',side_effect=observe):
   with self.assertRaises(LaunchObserved):runpy.run_path(str(root/'tests/scenario/capture.py'),run_name='__main__')
 def test_readiness_uses_the_owned_socket(self):
  from capture_listener import wait_listener,listener_address
  import socket
  child=subprocess.Popen([sys.executable,'-u','-c',"import socket,sys; s=socket.socket();s.bind(('127.0.0.1',0));s.listen();print(s.getsockname()[1],flush=True);sys.stdin.read()"],stdin=subprocess.PIPE,stdout=subprocess.PIPE,text=True)
  try:
   port=int(child.stdout.readline());self.assertEqual(wait_listener(child),('127.0.0.1',port))
   with socket.create_connection(('127.0.0.1',port)):pass
   # A socket in the namespace is insufficient without this child's descriptor.
   with tempfile.TemporaryDirectory() as temp:
    proc=pathlib.Path(temp);folder=proc/'123';(folder/'fd').mkdir(parents=True);(folder/'net').mkdir()
    table='header\n 0: 0100007F:1234 00000000:0000 0A 0 0 0 0 0 999\n'
    (folder/'net/tcp').write_text(table);self.assertIsNone(listener_address(123,proc))
    (folder/'fd/7').symlink_to('socket:[999]');self.assertEqual(listener_address(123,proc),('127.0.0.1',0x1234))
  finally:
   child.stdin.close();child.wait();child.stdout.close()
if __name__=='__main__':unittest.main()
