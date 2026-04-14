import paramiko
import time

host = "192.168.0.3"
port = 2222
username = "koreaf16"
password = "Gnttkak1!"

try:
    client = paramiko.SSHClient()
    client.set_missing_host_key_policy(paramiko.AutoAddPolicy())
    client.connect(host, port=port, username=username, password=password, timeout=10)
    
    print("Ensuring huggingface_hub is installed...")
    client.exec_command("pip3 install -U huggingface_hub")
    time.sleep(5)  # Wait for install
    
    model_name = "Qwen/Qwen2.5-3B-Instruct-AWQ"
    cmd = f"nohup python3 -m huggingface_hub.cli download {model_name} > download_qwen_awq.log 2>&1 & echo $!"
    print(f"Executing: {cmd}")
    
    stdin, stdout, stderr = client.exec_command(cmd)
    pid = stdout.read().decode().strip()
    print(f"4-bit AWQ Download started in background with PID: {pid}")
    
    client.close()
except Exception as e:
    print(f"Failed to connect or execute command: {e}")
