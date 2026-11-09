import os
import sys

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

os.environ.setdefault('API_SECRET_KEY', 'test-secret-key-1234567890')
os.environ.setdefault('ENABLE_API_KEY_AUTH', 'true')
os.environ.setdefault('COOKIE_SECURE', 'false')
os.environ.setdefault('DEBUG', 'false')
