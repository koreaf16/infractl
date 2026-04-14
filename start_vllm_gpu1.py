import paramiko

host = "192.168.0.3"
port = 2222
username = "koreaf16"
password = "Gnttkak1!"

try:
    client = paramiko.SSHClient()
    client.set_missing_host_key_policy(paramiko.AutoAddPolicy())
    client.connect(host, port=port, username=username, password=password, timeout=10)
    
    script_content = """#!/bin/bash
export PATH=$HOME/.local/bin:$PATH

echo "Starting vLLM on GPU 1 with 64K context..."
# Run using the python3 module that root is using, assuming it's installed globally
CUDA_VISIBLE_DEVICES=1 nohup /usr/bin/python3 -m vllm.entrypoints.openai.api_server \
  --model Qwen/Qwen2.5-3B-Instruct-AWQ \
  --quantization awq \
  --max-model-len 65536 \
  --host 0.0.0.0 \
  --port 8001 \
  --gpu-memory-utilization 0.9 > vllm_gpu1.log 2>&1 &
echo $! > vllm_gpu1.pid
echo "vLLM started in background."
"""
    
    sftp = client.open_sftp()
    with sftp.file('start_vllm_gpu1.sh', 'w') as f:
        f.write(script_content)
    sftp.close()
    
    print("Executing start script on remote...")
    stdin, stdout, stderr = client.exec_command("bash start_vllm_gpu1.sh")
    print(stdout.read().decode())
    print(stderr.read().decode())
    
    client.close()
except Exception as e:
    print(f"Failed to connect or execute command: {e}")
